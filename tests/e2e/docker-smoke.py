"""Verify an already built local image; clean up the test container and volume."""
import json
import pathlib
import subprocess
import tempfile
import time
import uuid


def docker(*args):
    return subprocess.check_output(['docker', *args], text=True, stderr=subprocess.STDOUT)


name = 'striem-feature-' + uuid.uuid4().hex[:12]
with tempfile.TemporaryDirectory(prefix='striem-docker-') as directory:
    root = pathlib.Path(directory)
    root.chmod(0o755)
    (root / 'events.ndjson').write_text('{"ts":"2024-01-01T00:00:00Z","host":"alpha"}\n')
    (root / 'config.yaml').write_text('''challengeName: Docker test
fullTextIndex: true
flag: flag{docker_pass}
questions:
  - id: host
    title: Host
    prompt: Identify the host.
    acceptedAnswers: [alpha]
datasets:
  - name: fixture
    table: Fixture
    path: events.ndjson
    source: docker
    timestampPath: ts
''')
    for path in root.iterdir():
        path.chmod(0o644)
    try:
        docker('run', '--detach', '--rm', '--name', name, '--network', 'none', '--read-only',
               '--tmpfs', '/tmp:rw,nosuid,size=64m', '--cap-drop', 'ALL',
               '--security-opt', 'no-new-privileges:true',
               '--mount', f'type=bind,src={directory},dst=/config,readonly',
               '--env', 'STRIEM_CONFIG=/config/config.yaml', 'striem-feature-test')

        def get(path):
            return docker('exec', name, 'wget', '-qO-', 'http://127.0.0.1:8080' + path)

        def ready():
            for _ in range(50):
                try:
                    assert json.loads(get('/api/ready'))['status'] == 'ok'
                    return
                except (subprocess.CalledProcessError, AssertionError):
                    time.sleep(0.2)
            raise AssertionError(docker('logs', name))

        def post(path, payload):
            return json.loads(docker('exec', name, 'wget', '-qO-',
                '--header=Content-Type: application/json', '--header=X-Striem-Request: 1',
                '--post-data=' + json.dumps(payload), 'http://127.0.0.1:8080' + path))

        ready()
        assert docker('exec', name, 'id', '-u').strip() != '0'
        assert '<title>' in get('/')
        # Binary content is checked inside the container without text decoding.
        docker('exec', name, 'wget', '-qO', '/tmp/font.woff2', 'http://127.0.0.1:8080/fonts/instrument-sans-var.woff2')
        assert int(docker('exec', name, 'stat', '-c', '%s', '/tmp/font.woff2')) > 1000
        assert post('/api/query', {'query': 'Fixture | search "alpha" | count'})['rows'][0]['Count'] == 1
        for kind, expected in [('innerunique', 1), ('rightsemi', 1), ('rightanti', 0)]:
            query = f'Fixture | join kind={kind} (Fixture) on host | count'
            assert post('/api/query', {'query': query})['rows'][0]['Count'] == expected
        assert post('/api/questions/host/answer', {'answer': 'alpha'})['state']['completed']
        docker('restart', name)
        ready()
        assert json.loads(get('/api/questions'))['flag'] == 'flag{docker_pass}'
        assert post('/api/query', {'query': 'Events | count'})['rows'][0]['Count'] == 1
        print('PASS: non-root read-only container, readiness, assets/fonts, FTS query, challenge, restart persistence')
    finally:
        subprocess.run(['docker', 'stop', name], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
