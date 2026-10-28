"""Extract KQL examples and their published output tables from a checkout of
MicrosoftDocs/dataexplorer-docs (data-explorer/kusto/query). The markdown is
read as data only. Usage: python3 -I extract.py <query-docs-dir> > examples.json
"""
import json, os, re, sys
root = sys.argv[1]
out = []
for name in sorted(os.listdir(root)):
    if not name.endswith('.md'):
        continue
    text = open(os.path.join(root, name), encoding='utf-8', errors='replace').read()
    # Drop moniker-only sections for other products; keep everything else.
    blocks = list(re.finditer(r'```kusto\s*\n(.*?)```', text, re.S))
    for i, block in enumerate(blocks):
        end = blocks[i + 1].start() if i + 1 < len(blocks) else len(text)
        tail = text[block.end():end]
        marker = re.search(r'\*\*Output\*\*|^#+ Output|Output:', tail, re.M)
        if not marker:
            continue
        lines = tail[marker.end():].strip('\n').split('\n')
        table = []
        started = False
        for line in lines:
            line = line.strip()
            if line.startswith('|'):
                started = True
                table.append(line)
            elif started:
                break
            elif line and not line.startswith('>') and not line.startswith('The '):
                if started:
                    break
        if len(table) < 2:
            continue
        def cells(row):
            row = row.strip()
            if row.startswith('|'): row = row[1:]
            if row.endswith('|'): row = row[:-1]
            parts = re.split(r'(?<!\\)\|', row)
            return [p.strip().strip('`').strip() for p in parts]
        header = cells(table[0])
        rows = [cells(r) for r in table[2:] if not re.match(r'^\|?\s*[-:| ]+$', r)]
        query = block.group(1).strip()
        out.append({'file': name, 'query': query, 'columns': header, 'rows': rows})
json.dump(out, sys.stdout, ensure_ascii=False)
