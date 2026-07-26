interface TabDefinition<Name extends string> {
  name: Name;
  tab: HTMLButtonElement;
  panel: HTMLElement;
}

export function renderTabSelection<Name extends string>(
  selectedName: Name,
  tabs: TabDefinition<Name>[],
): void {
  tabs.forEach(({ name, tab, panel }) => {
    const selected = name === selectedName;
    panel.classList.toggle('hidden', !selected);
    tab.classList.toggle('selected', selected);
    tab.setAttribute('aria-selected', String(selected));
    tab.tabIndex = selected ? 0 : -1;
  });
}

export function enableTabKeyboardNavigation(tablist: Element, selector: string): void {
  const tabs = Array.from(tablist.querySelectorAll<HTMLButtonElement>(selector));
  tablist.addEventListener('keydown', event => {
    if (!(event instanceof KeyboardEvent)) return;
    const current = tabs.indexOf(document.activeElement as HTMLButtonElement);
    if (current < 0 || tabs.length === 0) return;

    let next: number | undefined;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (current + 1) % tabs.length;
    if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (current - 1 + tabs.length) % tabs.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = tabs.length - 1;
    if (next === undefined) return;

    event.preventDefault();
    tabs[next]?.click();
    tabs[next]?.focus();
  });
}
