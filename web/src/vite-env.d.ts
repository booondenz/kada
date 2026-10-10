/// <reference types="vite/client" />

// The one build-time variable this app reads.
//
// Vite replaces `import.meta.env.VITE_*` with the value it was built with, so declaring it here is what
// turns a misspelled name into a compile error instead of an undefined at runtime - which would silently
// fall back to the development API and look like a broken deployment.
interface ImportMetaEnv {
  readonly VITE_API_URL?: string;
}
