import { LitElement, html, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';
import './components/button.js';
import styles from './styles.css' with { type: 'css' };

export { something } from 'other-module';

const lazyModule = import('./lazy.js');
