import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';

@customElement('app-footer')
export class AppFooter extends LitElement {
  render() {
    return html`<footer>&copy; 2026</footer>`;
  }
}
