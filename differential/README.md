# Differential test module

This nested module contains development-only compatibility oracles and
comparative benchmarks. It deliberately depends on `github.com/dlclark/regexp2`
to compare behavior, but that dependency is isolated from the published
`github.com/VillanCh/go-pcre2-lite` module graph.

Run the production module and the optional oracle suite separately:

```sh
go test ./...
(cd differential && go test ./...)
```
