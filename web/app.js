// Mappa WASM Web Application (ESM)

// Load the Go WASM runtime
await import('./wasm_exec.js');

// DOM Elements
const packageJsonInput = document.getElementById('package-json');
const cdnSelect = document.getElementById('cdn-select');
const conditionsInput = document.getElementById('conditions');
const includeDevCheckbox = document.getElementById('include-dev');
const generateBtn = document.getElementById('generate-btn');
const copyBtn = document.getElementById('copy-btn');
const outputPre = document.getElementById('output');
const outputCode = outputPre.querySelector('code');
const versionSpan = document.getElementById('version');

// State
let wasmReady = false;
let generating = false;

// Load WASM
async function initWasm() {
    try {
        const go = new Go();
        const result = await WebAssembly.instantiateStreaming(
            fetch('mappa.wasm'),
            go.importObject
        );
        go.run(result.instance);

        // Wait for mappa to be defined
        await new Promise(resolve => {
            const check = () => {
                if (typeof globalThis.mappa !== 'undefined') {
                    resolve();
                } else {
                    setTimeout(check, 10);
                }
            };
            check();
        });

        wasmReady = true;
        versionSpan.textContent = `WASM v${globalThis.mappa.version}`;
        generateBtn.disabled = false;
        outputCode.textContent = 'Ready. Enter a package.json and click Generate.';

    } catch (err) {
        console.error('Failed to load WASM:', err);
        outputCode.textContent = `Failed to load WASM: ${err.message}`;
    }
}

// Parse conditions string into array
function parseConditions(str) {
    return str.split(',')
        .map(s => s.trim())
        .filter(s => s.length > 0);
}

// Generate import map
async function generateImportMap() {
    if (!wasmReady || generating) return;

    const packageJsonStr = packageJsonInput.value.trim();
    if (!packageJsonStr) {
        outputCode.textContent = 'Please enter a package.json';
        return;
    }

    // Validate JSON
    try {
        JSON.parse(packageJsonStr);
    } catch (e) {
        outputCode.textContent = `Invalid JSON: ${e.message}`;
        return;
    }

    generating = true;
    generateBtn.disabled = true;
    copyBtn.disabled = true;
    generateBtn.textContent = 'Generating...';
    outputCode.textContent = 'Fetching package metadata...';

    try {
        const options = {
            cdn: cdnSelect.value,
            conditions: parseConditions(conditionsInput.value),
            includeDev: includeDevCheckbox.checked
        };

        const result = await globalThis.mappa.generate(packageJsonStr, options);
        outputCode.textContent = result;
        copyBtn.disabled = false;

    } catch (err) {
        console.error('Generation failed:', err);
        outputCode.textContent = `Error: ${err.message}`;
        copyBtn.disabled = true;
    } finally {
        generating = false;
        generateBtn.disabled = false;
        generateBtn.textContent = 'Generate Import Map';
    }
}

// Copy to clipboard
async function copyOutput() {
    if (generating) return;
    try {
        await navigator.clipboard.writeText(outputCode.textContent);
        const originalText = copyBtn.textContent;
        copyBtn.textContent = 'Copied!';
        setTimeout(() => {
            copyBtn.textContent = originalText;
        }, 2000);
    } catch (err) {
        console.error('Copy failed:', err);
    }
}

// Event listeners
generateBtn.addEventListener('click', generateImportMap);
copyBtn.addEventListener('click', copyOutput);

// Allow Ctrl+Enter to generate
packageJsonInput.addEventListener('keydown', (e) => {
    if (e.ctrlKey && e.key === 'Enter') {
        generateImportMap();
    }
});

// Initialize
initWasm();
