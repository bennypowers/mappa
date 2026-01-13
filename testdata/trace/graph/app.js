import { LitElement, html } from 'lit';
import { customElement } from 'lit/decorators.js';
import './components/header.js';
import './components/footer.js';

@customElement('my-app')
export class MyApp extends LitElement {
  render() {
    return html`
      <app-header></app-header>
      <main><slot></slot></main>
      <app-footer></app-footer>
    `;
  }
}
