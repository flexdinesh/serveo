# serve-openapi

`serve-openapi` starts a local web server that renders OpenAPI YAML and JSON documents with
[Scalar](https://scalar.com/products/api-references/integrations/html-js).

## Install

Go 1.25 or later is required.

```sh
go install github.com/flexdinesh/serve-openapi@latest
```

Render one document:

```sh
serve-openapi path/to/openapi.yaml
```

The server selects a free local port, prints its URL, and opens it in your default browser.

## Usage

```text
serve-openapi <spec-file> [spec-file...] [flags]
```

Render multiple documents in Scalar's document picker:

```sh
serve-openapi auth-openapi.yaml users-openapi.yaml
```

Override the API server used by Scalar's test-request UI:

```sh
serve-openapi oauth-openapi.yaml \
  --server https://auth.staging.setmore.com
```

By default, the serving root is the nearest common directory containing all entry documents.
Relative `$ref` files beneath that directory are available to Scalar. Set a wider root when a
reference reaches a parent directory:

```sh
serve-openapi api/openapi.yaml --root .
```

Flags:

```text
      --no-open        do not open the browser automatically
      --port int       local port; 0 selects a free port
      --root string    root directory for local files and relative $refs
      --server string  override the API server used for test requests
  -h, --help           help for serve-openapi
```

If the browser cannot be opened, `serve-openapi` prints a warning and keeps the server running.
Use `--no-open` to skip opening the browser, or `--port` to request a specific port.

Stop the server with `Ctrl-C`.

## Security and file scope

- Entry documents and files served beneath `--root` must have a `.yaml`, `.yml`, or `.json`
  extension. Other files are not exposed.
- Multiple entry documents are displayed separately; they are not merged.
- Relative `$ref` files are limited to the serving root. Traversal and symlinks that escape the
  resolved root are rejected.
- The HTTP server binds only to the loopback interface and validates the request host against its
  bound address. It is not a network server or reverse-proxy target; use the printed loopback URL.
- `--server` changes the API server used by Scalar's test-request UI. It does not change the local
  listening address.
- Backstage `$text` catalog entries are not discovered automatically.
- Remote references and test requests remain subject to browser CORS rules.

## Scalar dependency

The web page loads the pinned `@scalar/api-reference` version `1.64.1` from jsDelivr at runtime.
Viewing the rendered reference therefore requires internet access. Upgrading Scalar is a deliberate
source change so its pinned version and integrity metadata can be reviewed together.

## Development

Download dependencies:

```sh
go mod download
```

Build and run locally:

```sh
go build -o ./bin/serve-openapi .
./bin/serve-openapi path/to/openapi.yaml
```

Run the checks used in CI:

```sh
gofmt -d .
go mod verify
go test -race -cover ./...
go vet ./...
go build ./...
```

Install the local binary to your Go binary directory:

```sh
go install .
```

## Releases

Releases are created manually from `main` with a semantic-version tag such as `v0.1.0`. The release
workflow reruns tests, vet, and build before publishing a GitHub release with generated notes. It
publishes the source and tag, not prebuilt platform binaries.

## License

MIT
