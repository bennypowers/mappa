; Static import statements
; import foo from 'bar';
; import { foo } from 'bar';
; import 'bar';
(import_statement
  source: (string
    (string_fragment) @import.spec)) @import

; Dynamic imports: import('path')
(call_expression
  function: (import)
  arguments: (arguments
    (string
      (string_fragment) @dynamicImport.spec))) @dynamicImport

; Async dynamic imports: await import('path')
(await_expression
  (call_expression
    function: (import)
    arguments: (arguments
      (string
        (string_fragment) @dynamicImport.spec)))) @dynamicImport

; Re-exports: export { foo } from 'bar';
(export_statement
  source: (string
    (string_fragment) @reexport.spec)) @reexport
