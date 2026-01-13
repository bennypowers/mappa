import { LitElement, html, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import './nav.js';

@customElement('app-header')
export class AppHeader extends LitElement {
  @property() title = 'My App';

  render() {
    return html`
      <header>
        <h1>${this.title}</h1>
        <app-nav></app-nav>
      </header>
    `;
  }
}
