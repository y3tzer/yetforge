// Replace the launcher process with the committed, statically linked native engine.
// The engine owns the listener, shared store, registration and completion lifecycle.
import { fileURLToPath } from 'node:url';
const executable = fileURLToPath(new URL('./yetforge', import.meta.url));
process.execve(executable, [executable], process.env);
