# cablectl: Persistent Code Execution for LLM Runtimes

`cablectl` is a Go library designed to provide seamless and persistent code execution capabilities, primarily for integration into Large Language Model (LLM) runtimes. It aims to offer a robust environment for code execution that persists across multiple LLM inferences, effectively enhancing or replacing traditional function-calling mechanisms.

It's been inspired by multiple prior offenders:

1. [janpfeifer/gonb](https://github.com/janpfeifer/gonb)
2. [crackcomm/go-jupyter](https://github.com/crackcomm/go-jupyter)
3. but none really cut it.
