# Cable controller

![][1]

This repository implements the _action space_ for arbitrary cable implementations, such as those inspired by [CodeAct][2] paper from 2024. [Jupyter Enterprise Gateway][3] is the natural choice for this, considering the popularity of Jupyter notebooks, and how much of it has made it to pretrains. That said, async behaviour and forking—calls for a more dynamic runtime, capable of tracing, back-channel communication, and persistence.

The `cablectl` module currently supports `python3` kernel alone, for practical reasons, but this is a temporary limitation.

The gateway configuration is beyond the scope of this project. We would recommend implementing a custom provisioner that could take advantage of [gVisor][4] sandboxing, or something similar, and most importantly—to hide latency from the runtime—making sure to over-provision the kernels, informed by the runtime requirements (such as most-used tool profiles) and decomposing streaming code-blocks into multiple distinct actions.

Grammatical sampling is key to that end.

## Cables

Like many others in the industry, we have spent the last two years refining our LLM consumption: basically, re-implementing OpenAI API-compatible reverse proxies every other month. I'm only half-joking. This specific "API line" had proliferated as much, owing exclusively to ChatGPT effect, and literature supports this assessment; instruction-following was a hot research topic at the time, as well as reward modelling for code generation, but not the now-prevalent _helpful assistant_-style alignment, or the obsession with chat templates and LM Arena.

It was always common to spend lots of resources in post-training on formatting, but the obsession with OpenAPI schemas (JSON) took it to a new level. For structured outputs, this is reasonable, and indeed, you could do it very early on with something like GBNF via llama.cpp, or Lark, via vLLM. However, it never really made sense for tool-use. We have been training models to write perfectly good code, so why not let them do it? Instead, the API providers chose to split function-calling and, arguably, the overarching code interpreter—into two distinct interfaces. The traditional function-calling is _polluting_ context with JSON, but if your runtime could translate the OpenAPI function schema to a Python function, or what-have-you, then your agents could effectively "bootstrap" novel tools from it!

This presents a challenge as most state of the art LLM runtimes are either below the API line of a cable, or way above it.

## Prior art

We had originally expected to use some existing solution, considering Go is quite well-represented in the Jupyter ecosystem: most notably, [gonb][5] and [gophernotes][7]. Neither would offer a high-enough level of API to be useful for a highly-concurrent, persistent cable implementation. That is to say, neither really would work with [Jupyter Enterprise Gateway][3].

1. [janpfeifer/gonb][5] had implemented a kernel, but the good bits are all internal to it;
2. [crackcomm/go-jupyter][6] is good for some imports;
3. but none really cut it.

Besides the Jupyter protocol, there's more to a Cable controller than the kernel alone.

The cable semantics more-or-less abandon, or in some cases, abstract away the chat completion API's, function-calling, etc. The competent cable controller would also take care of bringing-up the action space, translate OpenAPI schemas normally used for function-calling, into Python functions. In the recent months, the pair of [MCP][8] for function discovery, and [A2A][9] for agent-to-agent communication, have emerged as the de facto standard means of tool-use over the network.

Short of a fully-fledged [Zanzibar][10] implementation, some form of Extended RBAC is within the scope of this project. We'll figure this out as soon as we have a working, grammar-based `OpenAPI <-> Kernel` translation layer. The field is evolving so fast, at this point, it's impossible to make assumptions about the penultimate implementation.

## LLM Ops

The cable semantics calls for strong LLM Ops so that the inference, and the action space could be traced simultaneously.

Besides kernel provisioning and execution, this project implements a [Langfuse][11] client, too. There are a few existing Go libraries that provide Langfuse tracing, but none are really ergonomic enough for cable-like data access patterns. We will maintain an up-to-date code-generated API client for it, however, for all intents and purposes, a higher-level API would be used typically.

## License

GNU General Public License v3.0 (GPLv3)

[1]: cables.png
[2]: https://arxiv.org/abs/2402.01030
[3]: https://jupyter-enterprise-gateway.readthedocs.io/en/latest/index.html
[4]: https://gvisor.dev/
[5]: https://github.com/janpfeifer/gonb
[6]: https://github.com/crackcomm/go-jupyter
[7]: https://github.com/gopherdata/gophernotes
[8]: https://modelcontextprotocol.io/
[9]: https://google.github.io/A2A/
[10]: https://www.usenix.org/conference/atc19/presentation/pang
[11]: https://langfuse.com/
