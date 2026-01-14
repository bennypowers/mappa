// Direct dependency - valid
import { html } from 'lit';

// Transitive dependency - issue
import { ifDefined } from 'lit-html/directives/if-defined.js';

// Dev dependency - issue
import { something } from 'typescript';

// Not installed - issue
import { foo } from 'not-installed-package';
