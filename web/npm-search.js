// <npm-search> web component
// Fires 'npm-add' CustomEvent with { name, version } detail

/** @tagName npm-search */
class NpmSearch extends HTMLElement {
  #activeIndex = -1;
  #selectedPackage = null;
  #searchController = null;
  #debounceId = null;
  #input; #listbox; #version; #addBtn; #combobox;

  static is = 'npm-search'
  static { customElements.define(this.is, this); }

  constructor() {
    super();
    const template = document.getElementById('npm-search-template');
    this.attachShadow({ mode: 'open' });
    this.shadowRoot.append(template.content.cloneNode(true));
    this.#input = this.shadowRoot.getElementById('input');
    this.#listbox = this.shadowRoot.getElementById('listbox');
    this.#version = this.shadowRoot.getElementById('version');
    this.#addBtn = this.shadowRoot.getElementById('add');
    this.#combobox = this.shadowRoot.querySelector('[role="combobox"]');
  }

  connectedCallback() {
    this.#input.addEventListener('input', () => this.#onInput());
    this.#input.addEventListener('keydown', e => this.#onKeydown(e));
    this.#input.addEventListener('blur', () => setTimeout(() => this.#hideListbox(), 200));
    this.#addBtn.addEventListener('click', () => this.#addPackage());
    this.#version.addEventListener('keydown', e => { if (e.key === 'Enter') this.#addPackage(); });
  }

  #onInput() {
    this.#selectedPackage = null;
    this.#version.innerHTML = '<option value="">version</option>';
    this.#version.disabled = true;
    this.#addBtn.disabled = true;

    clearTimeout(this.#debounceId);
    this.#searchController?.abort();
    const query = this.#input.value.trim();
    if (query.length < 2) {
      this.#hideListbox();
      return;
    }
    this.#debounceId = setTimeout(() => this.#search(query), 250);
  }

  #onKeydown(e) {
    const options = this.#listbox.querySelectorAll('[role="option"]');
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        if (!this.#listbox.hidden) this.#setActive(this.#activeIndex + 1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        if (!this.#listbox.hidden) this.#setActive(this.#activeIndex - 1);
        break;
      case 'Enter':
        e.preventDefault();
        if (this.#activeIndex >= 0 && options[this.#activeIndex])
          this.#selectPackage(options[this.#activeIndex].dataset.name);
        break;
      case 'Escape':
        this.#hideListbox();
        break;
    }
  }

  async #search(query) {
    if (this.#searchController) this.#searchController.abort();
    this.#searchController = new AbortController();
    try {
      const url = `https://registry.npmjs.org/-/v1/search?text=${encodeURIComponent(query)}&size=10`;
      const res = await fetch(url, { signal: this.#searchController.signal });
      const data = await res.json();
      if (this.#input.value.trim() !== query) return;
      this.#showResults(data.objects.map(o => o.package));
    } catch (err) {
      if (err.name !== 'AbortError') console.error('npm search failed:', err);
    }
  }

  #showResults(packages) {
    this.#listbox.innerHTML = '';
    this.#activeIndex = -1;
    if (!packages.length) { this.#hideListbox(); return; }
    for (const [i, pkg] of packages.entries()) {
      const li = document.createElement('li');
      li.role = 'option';
      li.id = `opt-${i}`;
      li.dataset.name = pkg.name;
      const nameEl = document.createElement('strong');
      nameEl.textContent = pkg.name;
      const versionEl = document.createElement('span');
      versionEl.textContent = pkg.version;
      li.append(nameEl, document.createTextNode(' '), versionEl);
      if (pkg.description) {
        li.append(document.createElement('br'));
        const desc = document.createElement('small');
        desc.textContent = pkg.description;
        li.append(desc);
      }
      li.addEventListener('click', () => this.#selectPackage(pkg.name));
      this.#listbox.append(li);
    }
    this.#listbox.hidden = false;
    this.#combobox.setAttribute('aria-expanded', 'true');
  }

  #hideListbox() {
    this.#listbox.hidden = true;
    this.#combobox.setAttribute('aria-expanded', 'false');
    this.#activeIndex = -1;
    this.#input.removeAttribute('aria-activedescendant');
  }

  #setActive(index) {
    const options = this.#listbox.querySelectorAll('[role="option"]');
    if (!options.length) return;
    for (const opt of options) opt.classList.remove('active');
    this.#activeIndex = ((index % options.length) + options.length) % options.length;
    options[this.#activeIndex].classList.add('active');
    options[this.#activeIndex].scrollIntoView({ block: 'nearest' });
    this.#input.setAttribute('aria-activedescendant', options[this.#activeIndex].id);
  }

  async #selectPackage(name) {
    this.#selectedPackage = name;
    this.#input.value = name;
    this.#hideListbox();
    this.#version.disabled = true;
    this.#version.innerHTML = '<option value="">loading...</option>';

    try {
      const url = `https://registry.npmjs.org/${encodeURIComponent(name)}`;
      const res = await fetch(url, { headers: { Accept: 'application/vnd.npm.install-v1+json' } });
      const data = await res.json();
      if (this.#selectedPackage !== name) return;
      const distTags = data['dist-tags'] || {};
      const versions = Object.keys(data.versions || {}).reverse();

      this.#version.innerHTML = '';
      const latest = distTags.latest;
      if (latest) {
        const opt = document.createElement('option');
        opt.value = `^${latest}`;
        opt.textContent = `^${latest} (latest)`;
        this.#version.append(opt);
      }
      for (const v of versions) {
        if (v === latest) continue;
        const opt = document.createElement('option');
        opt.value = v;
        opt.textContent = v;
        this.#version.append(opt);
      }
      this.#version.disabled = false;
      this.#addBtn.disabled = false;
    } catch (err) {
      if (this.#selectedPackage !== name) return;
      console.error('Failed to fetch versions:', err);
      this.#version.innerHTML = '<option value="">error</option>';
    }
  }

  #addPackage() {
    if (!this.#selectedPackage || this.#version.disabled) return;
    const version = this.#version.value;
    if (!version) return;

    this.dispatchEvent(new CustomEvent('npm-add', {
      bubbles: true,
      detail: { name: this.#selectedPackage, version }
    }));

    this.#selectedPackage = null;
    this.#input.value = '';
    this.#version.innerHTML = '<option value="">version</option>';
    this.#version.disabled = true;
    this.#addBtn.disabled = true;
  }
}

