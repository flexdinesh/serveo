# serve-openapi

A small local CLI that renders OpenAPI YAML and JSON documents with
[Scalar](https://scalar.com/products/api-references/integrations/html-js).

## Build

~~~bash
go build -o serve-openapi .
~~~

## Usage

~~~text
serve-openapi <spec-file> [spec-file...] [flags]
~~~

Render one document:

~~~bash
./serve-openapi path/to/openapi.yaml
~~~

Render multiple documents in Scalar's document picker:

~~~bash
./serve-openapi auth-openapi.yaml users-openapi.yaml
~~~

Override the API server used by Scalar's test-request UI:

~~~bash
./serve-openapi oauth-openapi.yaml \
  --server https://auth.staging.setmore.com
~~~

By default, the CLI serves the nearest common directory containing all entry
documents. Standard relative $ref files beneath that directory are available
to Scalar. Set a wider root when a reference reaches a parent directory:

~~~bash
./serve-openapi api/openapi.yaml --root .
~~~

Use --port 0 to select a free port, or --no-open to print the local URL
without opening a browser.

## Scope

- Entry documents must be local .yaml, .yml, or .json files.
- Multiple entry documents are displayed separately; they are not merged.
- Standard relative $ref files are served from --root.
- Backstage $text catalog entries are not discovered automatically.
- Remote references and test requests are subject to browser CORS rules.
- Scalar is loaded from jsDelivr, so rendering requires internet access.
