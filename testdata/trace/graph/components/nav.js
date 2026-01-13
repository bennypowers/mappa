import { LitElement, html } from 'lit';
import { customElement, state } from 'lit/decorators.js';
import { classMap } from 'lit/directives/class-map.js';

@customElement('app-nav')
export class AppNav extends LitElement {
  @state() active = 'home';

  render() {
    return html`
      <nav>
        <a class=${classMap({active: this.active === 'home'})} href="/">Home</a>
        <a class=${classMap({active: this.active === 'about'})} href="/about">About</a>
      </nav>
    `;
  }
}
