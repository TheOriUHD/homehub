# Third-Party Notices

HomeHub is published under the PolyForm Noncommercial License 1.0.0. It includes, links against or bundles the third-party components listed below, each of which remains under its own license; this file reproduces their notices as those licenses require. Generated from `go.mod`/`go.sum` (build graph of `./cmd/homehub`), `web/package-lock.json` (production dependency closure) and `companion/**/build.gradle`.

## Notes

- **Mojang artwork is not redistributed.** Minecraft item and GUI textures are never part of this repository or its binaries. `homehub minecraft-textures` downloads them from Mojang's own servers onto the operator's machine on request. The default player skin that ships with HomeHub is original placeholder art.
- **MPL-2.0 components** (`github.com/go-sql-driver/mysql`, `@novnc/novnc`) are used unmodified; their source is available from their upstream projects, and their license terms apply to those files.
- **The Minecraft companion plugins** are compiled against the Spigot API (GPL-3.0), Forge (LGPL-2.1) and the Minecraft client with Mojang's mappings (proprietary). These are compile-time dependencies only and are not bundled into the plugin jars; the only library shaded into a jar is `nv-websocket-client` (Apache-2.0).
- **Container images.** The repository's `Dockerfile` pulls `alpine`, `mariadb-client`, `postgresql17-client` and `mongodb-tools` at build time. Their licenses (including GPL-2.0 for some) apply to any image you build and redistribute.

## Summary

Go: 90 modules linked into `./cmd/homehub` (22 direct); npm: 365 production packages (of 530 lockfile entries; 164 are dev-only and are not distributed); Companion: 17 libraries/tools; data and fonts: 5 items.

| License | Go | npm | Companion | Data/fonts |
|---------|---:|----:|----------:|-----------:|
| MIT | 30 | 345 | 5 |  |
| Apache-2.0 | 26 | 2 | 8 |  |
| BSD-3-Clause | 18 | 3 |  |  |
| ISC | 2 | 11 |  |  |
| BSD-2-Clause | 4 | 1 |  |  |
| Apache-2.0 AND BSD-3-Clause | 4 |  |  |  |
| MPL-2.0 | 1 | 1 |  |  |
| MIT AND Apache-2.0 | 2 |  |  |  |
| LGPL-2.1 |  |  | 2 |  |
| OFL-1.1 |  |  |  | 2 |
| BSD-3-Clause AND BSD-2-Clause | 1 |  |  |  |
| BSD-3-Clause AND MIT AND Apache-2.0 | 1 |  |  |  |
| MIT AND BSD-3-Clause | 1 |  |  |  |
| Unlicense |  | 1 |  |  |
| 0BSD |  | 1 |  |  |
| GPL-3.0 |  |  | 1 |  |
| Proprietary (Minecraft EULA) |  |  | 1 |  |
| Public registry data (IEEE Registration Authority) |  |  |  | 1 |
| Proprietary (Mojang Studios / Microsoft) |  |  |  | 1 |
| First-party |  |  |  | 1 |
| **Total** | **90** | **365** | **17** | **5** |

Notes: compound entries (`A AND B`) are files containing several licenses; the Go total is 89 modules on linux/darwin plus `go-winio` that is only compiled for Windows. Licenses were read from each module's/package's own LICENSE file, and cross-checked against the `license` field of `package.json` for npm.

## Go modules

Source: `go list -deps ./cmd/homehub` (tags `ui companion helper`, GOOS linux/darwin/windows); license files read from the module cache. `helper/caddy-reader` is a separate Go module that imports only the standard library. The Go toolchain/standard library itself (BSD-3-Clause, The Go Authors) is also linked into the binary; see the BSD-3-Clause text below.

### Apache-2.0 (26)

- github.com/distribution/reference v0.6.0 — Apache-2.0 — https://github.com/distribution/reference
- github.com/docker/docker v26.1.5+incompatible — Apache-2.0 — https://github.com/docker/docker (direct)
- github.com/docker/go-connections v0.5.0 — Apache-2.0 — https://github.com/docker/go-connections
- github.com/docker/go-units v0.5.0 — Apache-2.0 — https://github.com/docker/go-units
- github.com/go-logr/logr v1.4.4 — Apache-2.0 — https://github.com/go-logr/logr
- github.com/go-logr/stdr v1.2.2 — Apache-2.0 — https://github.com/go-logr/stdr
- github.com/go-openapi/jsonpointer v0.23.1 — Apache-2.0 — https://github.com/go-openapi/jsonpointer
- github.com/go-openapi/swag/jsonname v0.26.0 — Apache-2.0 — https://github.com/go-openapi/swag
- github.com/google/go-tpm v0.9.8 — Apache-2.0 — https://github.com/google/go-tpm
- github.com/minio/crc64nvme v1.1.1 — Apache-2.0 — https://github.com/minio/crc64nvme
- github.com/minio/md5-simd v1.1.2 — Apache-2.0 — https://github.com/minio/md5-simd
- github.com/minio/minio-go/v7 v7.3.0 — Apache-2.0 — https://github.com/minio/minio-go (direct)
- github.com/moby/docker-image-spec v1.3.1 — Apache-2.0 — https://github.com/moby/docker-image-spec
- github.com/oapi-codegen/runtime v1.7.0 — Apache-2.0 — https://github.com/oapi-codegen/runtime (direct)
- github.com/opencontainers/go-digest v1.0.0 — Apache-2.0 — https://github.com/opencontainers/go-digest
- github.com/opencontainers/image-spec v1.1.1 — Apache-2.0 — https://github.com/opencontainers/image-spec
- github.com/pquerna/otp v1.5.0 — Apache-2.0 — https://github.com/pquerna/otp (direct)
- github.com/richardlehane/mscfb v1.0.7 — Apache-2.0 — https://github.com/richardlehane/mscfb
- github.com/richardlehane/msoleps v1.0.6 — Apache-2.0 — https://github.com/richardlehane/msoleps
- github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 — Apache-2.0 — https://github.com/santhosh-tekuri/jsonschema
- github.com/sethvargo/go-retry v0.4.0 — Apache-2.0 — https://github.com/sethvargo/go-retry
- github.com/xdg-go/scram v1.2.0 — Apache-2.0 — https://github.com/xdg-go/scram
- github.com/xdg-go/stringprep v1.0.4 — Apache-2.0 — https://github.com/xdg-go/stringprep
- go.mongodb.org/mongo-driver/v2 v2.8.0 — Apache-2.0 — https://github.com/mongodb/mongo-go-driver (direct)
- go.opentelemetry.io/auto/sdk v1.2.1 — Apache-2.0 — https://github.com/open-telemetry/opentelemetry-go-instrumentation
- gopkg.in/ini.v1 v1.67.3 — Apache-2.0 — https://github.com/go-ini/ini

### MIT (30)

- github.com/apapsch/go-jsonmerge/v2 v2.0.0 — MIT — https://github.com/apapsch/go-jsonmerge
- github.com/boombuler/barcode v1.0.1-0.20190219062509-6c824513bacc — MIT — https://github.com/boombuler/barcode
- github.com/cespare/xxhash/v2 v2.3.0 — MIT — https://github.com/cespare/xxhash
- github.com/dustin/go-humanize v1.0.1 — MIT — https://github.com/dustin/go-humanize
- github.com/emersion/go-ical v0.0.0-20250609112844-439c63cef608 — MIT — https://github.com/emersion/go-ical
- github.com/emersion/go-webdav v0.7.0 — MIT — https://github.com/emersion/go-webdav
- github.com/felixge/httpsnoop v1.1.0 — MIT — https://github.com/felixge/httpsnoop
- github.com/fxamacker/cbor/v2 v2.9.2 — MIT — https://github.com/fxamacker/cbor
- github.com/gabriel-vasile/mimetype v1.4.15 — MIT — https://github.com/gabriel-vasile/mimetype (direct)
- github.com/getkin/kin-openapi v0.142.0 — MIT — https://github.com/getkin/kin-openapi (direct)
- github.com/go-chi/chi/v5 v5.3.1 — MIT — https://github.com/go-chi/chi (direct)
- github.com/go-viper/mapstructure/v2 v2.5.0 — MIT — https://github.com/go-viper/mapstructure
- github.com/golang-jwt/jwt/v5 v5.3.1 — MIT — https://github.com/golang-jwt/jwt
- github.com/jackc/pgpassfile v1.0.0 — MIT — https://github.com/jackc/pgpassfile
- github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 — MIT — https://github.com/jackc/pgservicefile
- github.com/jackc/pgx/v5 v5.10.0 — MIT — https://github.com/jackc/pgx (direct)
- github.com/jackc/puddle/v2 v2.2.2 — MIT — https://github.com/jackc/puddle
- github.com/klauspost/cpuid/v2 v2.4.0 — MIT — https://github.com/klauspost/cpuid
- github.com/mfridman/interpolate v0.0.2 — MIT — https://github.com/mfridman/interpolate
- github.com/Microsoft/go-winio v0.6.2 — MIT — https://github.com/microsoft/go-winio (Windows-only via docker/go-connections; license per upstream, module not in local cache)
- github.com/philhofer/fwd v1.2.0 — MIT — https://github.com/philhofer/fwd
- github.com/pressly/goose/v3 v3.26.0 — MIT — https://github.com/pressly/goose (direct)
- github.com/robfig/cron/v3 v3.0.1 — MIT — https://github.com/robfig/cron (direct)
- github.com/rs/xid v1.6.0 — MIT — https://github.com/rs/xid
- github.com/teambition/rrule-go v1.8.2 — MIT — https://github.com/teambition/rrule-go
- github.com/tiendc/go-deepcopy v1.7.2 — MIT — https://github.com/tiendc/go-deepcopy
- github.com/tinylib/msgp v1.6.4 — MIT — https://github.com/tinylib/msgp
- github.com/x448/float16 v0.8.4 — MIT — https://github.com/x448/float16
- github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 — MIT — https://github.com/youmark/pkcs8
- go.uber.org/multierr v1.11.0 — MIT — https://github.com/uber-go/multierr

### BSD-3-Clause (18)

- filippo.io/age v1.3.1 — BSD-3-Clause — https://github.com/FiloSottile/age (direct)
- filippo.io/edwards25519 v1.2.0 — BSD-3-Clause — https://github.com/FiloSottile/edwards25519
- filippo.io/hpke v0.4.0 — BSD-3-Clause — https://github.com/FiloSottile/hpke
- github.com/geoffgarside/ber v1.1.0 — BSD-3-Clause — https://github.com/geoffgarside/ber
- github.com/go-webauthn/webauthn v0.17.4 — BSD-3-Clause — https://github.com/go-webauthn/webauthn (direct)
- github.com/gogo/protobuf v1.3.2 — BSD-3-Clause — https://github.com/gogo/protobuf
- github.com/google/uuid v1.6.0 — BSD-3-Clause — https://github.com/google/uuid (direct)
- github.com/klauspost/crc32 v1.3.0 — BSD-3-Clause — https://github.com/klauspost/crc32
- github.com/miekg/dns v1.1.73 — BSD-3-Clause — https://github.com/miekg/dns
- github.com/xuri/efp v0.0.1 — BSD-3-Clause — https://github.com/xuri/efp
- github.com/xuri/excelize/v2 v2.11.0 — BSD-3-Clause — https://github.com/xuri/excelize (direct)
- github.com/xuri/nfp v0.0.2-0.20250530014748-2ddeb826f9a9 — BSD-3-Clause — https://github.com/xuri/nfp
- golang.org/x/crypto v0.55.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/crypto (direct)
- golang.org/x/net v0.58.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/net
- golang.org/x/sync v0.22.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/sync
- golang.org/x/sys v0.47.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/sys (direct)
- golang.org/x/text v0.41.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/text
- golang.org/x/time v0.14.0 — BSD-3-Clause — https://pkg.go.dev/golang.org/x/time (direct)

### BSD-2-Clause (4)

- github.com/hirochachacha/go-smb2 v1.1.0 — BSD-2-Clause — https://github.com/hirochachacha/go-smb2 (direct)
- github.com/pkg/errors v0.9.1 — BSD-2-Clause — https://github.com/pkg/errors
- github.com/vmware/go-nfs-client v0.0.0-20190605212624-d43b92724c1b — BSD-2-Clause — https://github.com/vmware/go-nfs-client (direct)
- github.com/zeebo/xxh3 v1.1.0 — BSD-2-Clause — https://github.com/zeebo/xxh3

### ISC (2)

- github.com/coder/websocket v1.8.15 — ISC — https://github.com/coder/websocket (direct)
- github.com/rasky/go-xdr v0.0.0-20170124162913-1a41d1a06c93 — ISC — https://github.com/rasky/go-xdr

### MPL-2.0 (1)

- github.com/go-sql-driver/mysql v1.10.0 — MPL-2.0 — https://github.com/go-sql-driver/mysql (direct)

### Apache-2.0 AND BSD-3-Clause (4)

- go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 — Apache-2.0 AND BSD-3-Clause — https://github.com/open-telemetry/opentelemetry-go-contrib
- go.opentelemetry.io/otel v1.45.0 — Apache-2.0 AND BSD-3-Clause — https://github.com/open-telemetry/opentelemetry-go
- go.opentelemetry.io/otel/metric v1.45.0 — Apache-2.0 AND BSD-3-Clause — https://github.com/open-telemetry/opentelemetry-go
- go.opentelemetry.io/otel/trace v1.45.0 — Apache-2.0 AND BSD-3-Clause — https://github.com/open-telemetry/opentelemetry-go

### BSD-3-Clause AND BSD-2-Clause (1)

- github.com/go-webauthn/x v0.2.6 — BSD-3-Clause AND BSD-2-Clause — https://github.com/go-webauthn/x

### BSD-3-Clause AND MIT AND Apache-2.0 (1)

- github.com/klauspost/compress v1.19.2 — BSD-3-Clause AND MIT AND Apache-2.0 — https://github.com/klauspost/compress (gzhttp (Apache-2.0) is not linked)

### MIT AND Apache-2.0 (2)

- github.com/oasdiff/yaml3 v0.0.14 — MIT AND Apache-2.0 — https://github.com/oasdiff/yaml3
- go.yaml.in/yaml/v3 v3.0.5 — MIT AND Apache-2.0 — https://github.com/yaml/go-yaml

### MIT AND BSD-3-Clause (1)

- github.com/oasdiff/yaml v0.1.1 — MIT AND BSD-3-Clause — https://github.com/oasdiff/yaml

Modules named in `go.mod` but not linked into the binary (build, code-generation or test-time only, not distributed; most are not in the local module cache so not verified here): `github.com/oapi-codegen/oapi-codegen/v2` (the `tool` directive), `golang.org/x/mod`, `golang.org/x/tools`, `github.com/speakeasy-api/{jsonpath,openapi}`, `github.com/dprotaso/go-yit`, `github.com/vmware-labs/yaml-jsonpath`, `go.opentelemetry.io/otel/{sdk,sdk/metric,exporters/...}`, `gotest.tools/v3`, `gopkg.in/yaml.v3`, `github.com/containerd/log`, `github.com/moby/term`, `github.com/morikuni/aec`, `github.com/xdg-go/pbkdf2`. None is known to be copyleft.

## Frontend (npm, `web/`)

Production dependency closure of `web/package.json` computed from `package-lock.json` (365 packages), license field and LICENSE file read from `web/node_modules`. Dev-only build tooling (vite, tailwindcss, typescript, openapi-typescript, esbuild/rollup binaries and similar) is not distributed; among those, `lightningcss` is MPL-2.0, `argparse` is Python-2.0 and `caniuse-lite` is CC-BY-4.0, none of which end up in the shipped bundle. Not every package below is necessarily bundled into `web/dist` (for example Node-only helpers pulled in by `qrcode`'s CLI).

### Apache-2.0 (2)

- dompurify 3.4.13 — Apache-2.0 (elected; dual MPL-2.0 OR Apache-2.0) — https://github.com/cure53/DOMPurify
- typescript 5.9.3 — Apache-2.0 — https://www.typescriptlang.org/

### MIT (345)

- @babel/helper-string-parser 7.29.7 — MIT — https://babel.dev/docs/en/next/babel-helper-string-parser
- @babel/helper-validator-identifier 7.29.7 — MIT — https://github.com/babel/babel
- @babel/parser 7.29.8 — MIT — https://babel.dev/docs/en/next/babel-parser
- @babel/runtime 7.29.7 — MIT — https://babel.dev/docs/en/next/babel-runtime
- @babel/types 7.29.8 — MIT — https://babel.dev/docs/en/next/babel-types
- @codemirror/autocomplete 6.20.3 — MIT — https://code.haverbeke.berlin/codemirror/autocomplete
- @codemirror/commands 6.11.0 — MIT — https://code.haverbeke.berlin/codemirror/commands
- @codemirror/lang-angular 0.1.4 — MIT — https://github.com/codemirror/lang-angular
- @codemirror/lang-cpp 6.0.3 — MIT — https://github.com/codemirror/lang-cpp
- @codemirror/lang-css 6.3.1 — MIT — https://github.com/codemirror/lang-css
- @codemirror/lang-go 6.0.1 — MIT — https://github.com/codemirror/lang-go
- @codemirror/lang-html 6.4.12 — MIT — https://code.haverbeke.berlin/codemirror/lang-html
- @codemirror/lang-java 6.0.2 — MIT — https://github.com/codemirror/lang-java
- @codemirror/lang-javascript 6.2.5 — MIT — https://github.com/codemirror/lang-javascript
- @codemirror/lang-jinja 6.0.1 — MIT — https://code.haverbeke.berlin/codemirror/lang-jinja
- @codemirror/lang-json 6.0.2 — MIT — https://github.com/codemirror/lang-json
- @codemirror/lang-less 6.0.2 — MIT — https://github.com/codemirror/lang-less
- @codemirror/lang-liquid 6.3.2 — MIT — https://github.com/codemirror/lang-liquid
- @codemirror/lang-markdown 6.5.2 — MIT — https://code.haverbeke.berlin/codemirror/lang-markdown
- @codemirror/lang-php 6.0.2 — MIT — https://github.com/codemirror/lang-php
- @codemirror/lang-python 6.2.1 — MIT — https://github.com/codemirror/lang-python
- @codemirror/lang-rust 6.0.2 — MIT — https://github.com/codemirror/lang-rust
- @codemirror/lang-sass 6.0.2 — MIT — https://github.com/codemirror/lang-sass
- @codemirror/lang-sql 6.10.0 — MIT — https://github.com/codemirror/lang-sql
- @codemirror/lang-vue 0.1.3 — MIT — https://github.com/codemirror/lang-vue
- @codemirror/lang-wast 6.0.2 — MIT — https://github.com/codemirror/lang-wast
- @codemirror/lang-xml 6.1.0 — MIT — https://github.com/codemirror/lang-xml
- @codemirror/lang-yaml 6.1.3 — MIT — https://github.com/codemirror/lang-yaml
- @codemirror/language 6.12.4 — MIT — https://code.haverbeke.berlin/codemirror/language
- @codemirror/language-data 6.5.2 — MIT — https://github.com/codemirror/language-data
- @codemirror/legacy-modes 6.5.3 — MIT — https://code.haverbeke.berlin/codemirror/legacy-modes
- @codemirror/lint 6.9.7 — MIT — https://code.haverbeke.berlin/codemirror/lint
- @codemirror/search 6.7.1 — MIT — https://code.haverbeke.berlin/codemirror/search
- @codemirror/state 6.7.1 — MIT — https://code.haverbeke.berlin/codemirror/state
- @codemirror/theme-one-dark 6.1.3 — MIT — https://github.com/codemirror/theme-one-dark
- @codemirror/view 6.43.9 — MIT — https://code.haverbeke.berlin/codemirror/view
- @floating-ui/core 1.8.0 — MIT — https://floating-ui.com
- @floating-ui/dom 1.8.0 — MIT — https://floating-ui.com
- @floating-ui/react-dom 2.1.9 — MIT — https://floating-ui.com/docs/react-dom
- @floating-ui/utils 0.2.12 — MIT — https://floating-ui.com
- @jridgewell/sourcemap-codec 1.5.5 — MIT — https://github.com/jridgewell/sourcemaps/tree/main/packages/sourcemap-codec
- @lezer/common 1.5.2 — MIT — https://github.com/lezer-parser/common
- @lezer/cpp 1.1.6 — MIT — https://code.haverbeke.berlin/lezer/cpp
- @lezer/css 1.3.6 — MIT — https://code.haverbeke.berlin/lezer/css
- @lezer/go 1.0.1 — MIT — https://github.com/lezer-parser/go
- @lezer/highlight 1.2.3 — MIT — https://github.com/lezer-parser/highlight
- @lezer/html 1.3.13 — MIT — https://github.com/lezer-parser/html
- @lezer/java 1.1.3 — MIT — https://github.com/lezer-parser/java
- @lezer/javascript 1.5.4 — MIT — https://github.com/lezer-parser/javascript
- @lezer/json 1.0.3 — MIT — https://github.com/lezer-parser/json
- @lezer/lr 1.4.10 — MIT — https://code.haverbeke.berlin/lezer/lr
- @lezer/markdown 1.7.2 — MIT — https://code.haverbeke.berlin/lezer/markdown
- @lezer/php 1.0.5 — MIT — https://github.com/lezer-parser/php
- @lezer/python 1.1.19 — MIT — https://code.haverbeke.berlin/lezer/python
- @lezer/rust 1.0.2 — MIT — https://github.com/lezer-parser/rust
- @lezer/sass 1.1.0 — MIT — https://github.com/lezer-parser/sass
- @lezer/xml 1.0.6 — MIT — https://github.com/lezer-parser/xml
- @lezer/yaml 1.0.4 — MIT — https://github.com/lezer-parser/yaml
- @marijn/find-cluster-break 1.0.3 — MIT — https://code.haverbeke.berlin/marijn/find-cluster-break
- @milkdown/components 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/core 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/crepe 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/ctx 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/exception 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/kit 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-block 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-clipboard 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-cursor 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-diff 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-history 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-indent 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-listener 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-slash 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-streaming 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-tooltip 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-trailing 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/plugin-upload 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/preset-commonmark 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/preset-gfm 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/prose 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/transformer 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @milkdown/utils 7.22.1 — MIT — https://github.com/Milkdown/milkdown
- @ocavue/utils 1.7.0 — MIT — https://github.com/ocavue/utils
- @radix-ui/number 1.1.3 — MIT — https://radix-ui.com/primitives
- @radix-ui/primitive 1.1.7 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-arrow 1.1.15 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-checkbox 1.3.11 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-collection 1.1.15 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-compose-refs 1.1.5 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-context 1.2.2 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-dialog 1.1.23 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-direction 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-dismissable-layer 1.1.19 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-dropdown-menu 2.1.24 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-focus-guards 1.1.6 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-focus-scope 1.1.16 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-id 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-menu 2.1.24 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-popover 1.1.23 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-popper 1.3.7 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-portal 1.1.17 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-presence 1.1.10 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-primitive 2.1.10 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-roving-focus 1.1.19 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-select 2.3.7 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-slot 1.3.3 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-switch 1.3.7 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-tabs 1.1.21 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-tooltip 1.2.16 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-callback-ref 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-controllable-state 1.2.6 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-effect-event 0.0.5 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-is-hydrated 0.1.3 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-layout-effect 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-previous 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-rect 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-use-size 1.1.4 — MIT — https://radix-ui.com/primitives
- @radix-ui/react-visually-hidden 1.2.11 — MIT — https://radix-ui.com/primitives
- @radix-ui/rect 1.1.3 — MIT — https://radix-ui.com/primitives
- @simplewebauthn/browser 13.3.0 — MIT — https://github.com/MasterKale/SimpleWebAuthn/tree/master/packages/browser
- @tanstack/history 1.162.1 — MIT — https://tanstack.com/router
- @tanstack/query-core 5.101.4 — MIT — https://tanstack.com/query
- @tanstack/react-query 5.101.4 — MIT — https://tanstack.com/query
- @tanstack/react-router 1.170.29 — MIT — https://tanstack.com/router
- @tanstack/react-store 0.9.3 — MIT — https://tanstack.com/store
- @tanstack/router-core 1.171.24 — MIT — https://tanstack.com/router
- @tanstack/store 0.9.3 — MIT — https://tanstack.com/store
- @types/debug 4.1.13 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/debug
- @types/estree 1.0.9 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/estree
- @types/estree-jsx 1.0.5 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/estree-jsx
- @types/hast 3.0.5 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/hast
- @types/katex 0.16.8 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/katex
- @types/lodash 4.17.25 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/lodash
- @types/lodash-es 4.17.12 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/lodash-es
- @types/mdast 4.0.4 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/mdast
- @types/ms 2.1.0 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/ms
- @types/react 19.2.18 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/react
- @types/react-dom 19.2.4 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/react-dom
- @types/trusted-types 2.0.7 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/trusted-types (optional)
- @types/unist 3.0.3 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/unist
- @types/unist 2.0.11 — MIT — https://github.com/DefinitelyTyped/DefinitelyTyped/tree/master/types/unist
- @vue/compiler-core 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/compiler-core
- @vue/compiler-dom 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/compiler-dom
- @vue/compiler-sfc 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/compiler-sfc
- @vue/compiler-ssr 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/compiler-ssr
- @vue/reactivity 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/reactivity
- @vue/runtime-core 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/runtime-core
- @vue/runtime-dom 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/runtime-dom
- @vue/server-renderer 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/server-renderer
- @vue/shared 3.5.41 — MIT — https://github.com/vuejs/core/tree/main/packages/shared
- @xterm/addon-fit 0.11.0 — MIT — https://github.com/xtermjs/xterm.js/tree/master/addons/addon-fit
- @xterm/addon-search 0.16.0 — MIT — https://github.com/xtermjs/xterm.js/tree/master/addons/addon-search
- @xterm/xterm 6.0.0 — MIT — https://github.com/xtermjs/xterm.js
- ansi-regex 5.0.1 — MIT — https://github.com/chalk/ansi-regex
- ansi-styles 4.3.0 — MIT — https://github.com/chalk/ansi-styles
- aria-hidden 1.2.6 — MIT — https://github.com/theKashey/aria-hidden
- bail 2.0.2 — MIT — https://github.com/wooorm/bail
- camelcase 5.3.1 — MIT — https://github.com/sindresorhus/camelcase
- ccount 2.0.1 — MIT — https://github.com/wooorm/ccount
- character-entities 2.0.2 — MIT — https://github.com/wooorm/character-entities
- character-entities-html4 2.1.0 — MIT — https://github.com/wooorm/character-entities-html4
- character-entities-legacy 3.0.0 — MIT — https://github.com/wooorm/character-entities-legacy
- character-reference-invalid 2.0.1 — MIT — https://github.com/wooorm/character-reference-invalid
- clsx 2.1.1 — MIT — https://github.com/lukeed/clsx
- cmdk 1.1.1 — MIT — https://github.com/pacocoursey/cmdk
- codemirror 6.0.2 — MIT — https://github.com/codemirror/basic-setup
- color-convert 2.0.1 — MIT — https://github.com/Qix-/color-convert
- color-name 1.1.4 — MIT — https://github.com/colorjs/color-name
- comma-separated-tokens 2.0.3 — MIT — https://github.com/wooorm/comma-separated-tokens
- commander 8.3.0 — MIT — https://github.com/tj/commander.js
- cookie-es 3.1.1 — MIT — https://github.com/unjs/cookie-es
- crelt 1.0.7 — MIT — https://code.haverbeke.berlin/marijn/crelt
- cron-parser 5.10.0 — MIT — https://github.com/harrisiirak/cron-parser
- cronstrue 3.24.0 — MIT — https://github.com/bradymholt/cronstrue
- csstype 3.2.3 — MIT — https://github.com/frenic/csstype
- debug 4.4.3 — MIT — https://github.com/debug-js/debug
- decamelize 1.2.0 — MIT — https://github.com/sindresorhus/decamelize
- decode-named-character-reference 1.3.0 — MIT — https://github.com/wooorm/decode-named-character-reference
- dequal 2.0.3 — MIT — https://github.com/lukeed/dequal
- detect-node-es 1.1.0 — MIT — https://github.com/thekashey/detect-node
- devlop 1.1.0 — MIT — https://github.com/wooorm/devlop
- dijkstrajs 1.0.3 — MIT — https://github.com/tcort/dijkstrajs
- emoji-regex 8.0.0 — MIT — https://mths.be/emoji-regex
- escape-string-regexp 5.0.0 — MIT — https://github.com/sindresorhus/escape-string-regexp
- estree-util-is-identifier-name 3.0.0 — MIT — https://github.com/syntax-tree/estree-util-is-identifier-name
- estree-walker 2.0.2 — MIT — https://github.com/Rich-Harris/estree-walker
- exifr 7.1.3 — MIT — https://mutiny.cz/exifr/
- extend 3.0.2 — MIT — https://github.com/justmoon/node-extend
- find-up 4.1.0 — MIT — https://github.com/sindresorhus/find-up
- get-nonce 1.0.1 — MIT — https://github.com/theKashey/get-nonce
- hast-util-is-element 3.0.0 — MIT — https://github.com/syntax-tree/hast-util-is-element
- hast-util-to-jsx-runtime 2.3.6 — MIT — https://github.com/syntax-tree/hast-util-to-jsx-runtime
- hast-util-to-text 4.0.2 — MIT — https://github.com/syntax-tree/hast-util-to-text
- hast-util-whitespace 3.0.0 — MIT — https://github.com/syntax-tree/hast-util-whitespace
- html-parse-stringify 3.1.0 — MIT — https://github.com/henrikjoreteg/html-parse-stringify
- html-url-attributes 3.0.1 — MIT — https://github.com/rehypejs/rehype-minify/tree/main/packages/html-url-attributes
- i18next 24.2.3 — MIT — https://www.i18next.com
- inline-style-parser 0.2.7 — MIT — https://github.com/remarkablemark/inline-style-parser
- is-alphabetical 2.0.1 — MIT — https://github.com/wooorm/is-alphabetical
- is-alphanumerical 2.0.1 — MIT — https://github.com/wooorm/is-alphanumerical
- is-decimal 2.0.1 — MIT — https://github.com/wooorm/is-decimal
- is-fullwidth-code-point 3.0.0 — MIT — https://github.com/sindresorhus/is-fullwidth-code-point
- is-hexadecimal 2.0.1 — MIT — https://github.com/wooorm/is-hexadecimal
- is-plain-obj 4.1.0 — MIT — https://github.com/sindresorhus/is-plain-obj
- js-sha1 0.7.0 — MIT — https://github.com/emn178/js-sha1
- js-sha256 1.0.0 — MIT — https://github.com/emn178/js-sha256
- js-sha512 0.9.0 — MIT — https://github.com/emn178/js-sha512
- katex 0.18.4 — MIT — https://katex.org
- katex 0.16.47 — MIT — https://katex.org
- locate-path 5.0.0 — MIT — https://github.com/sindresorhus/locate-path
- lodash-es 4.18.1 — MIT — https://lodash.com/custom-builds
- longest-streak 3.1.0 — MIT — https://github.com/wooorm/longest-streak
- lowlight 3.3.0 — MIT — https://github.com/wooorm/lowlight
- luxon 3.7.2 — MIT — https://github.com/moment/luxon
- magic-string 0.30.21 — MIT — https://github.com/Rich-Harris/magic-string
- markdown-table 3.0.4 — MIT — https://github.com/wooorm/markdown-table
- mdast-util-definitions 6.0.0 — MIT — https://github.com/syntax-tree/mdast-util-definitions
- mdast-util-find-and-replace 3.0.2 — MIT — https://github.com/syntax-tree/mdast-util-find-and-replace
- mdast-util-from-markdown 2.0.3 — MIT — https://github.com/syntax-tree/mdast-util-from-markdown
- mdast-util-gfm 3.1.0 — MIT — https://github.com/syntax-tree/mdast-util-gfm
- mdast-util-gfm-autolink-literal 2.0.1 — MIT — https://github.com/syntax-tree/mdast-util-gfm-autolink-literal
- mdast-util-gfm-footnote 2.1.0 — MIT — https://github.com/syntax-tree/mdast-util-gfm-footnote
- mdast-util-gfm-strikethrough 2.0.0 — MIT — https://github.com/syntax-tree/mdast-util-gfm-strikethrough
- mdast-util-gfm-table 2.0.0 — MIT — https://github.com/syntax-tree/mdast-util-gfm-table
- mdast-util-gfm-task-list-item 2.0.0 — MIT — https://github.com/syntax-tree/mdast-util-gfm-task-list-item
- mdast-util-math 3.0.0 — MIT — https://github.com/syntax-tree/mdast-util-math
- mdast-util-mdx-expression 2.0.1 — MIT — https://github.com/syntax-tree/mdast-util-mdx-expression
- mdast-util-mdx-jsx 3.2.0 — MIT — https://github.com/syntax-tree/mdast-util-mdx-jsx
- mdast-util-mdxjs-esm 2.0.1 — MIT — https://github.com/syntax-tree/mdast-util-mdxjs-esm
- mdast-util-phrasing 4.1.0 — MIT — https://github.com/syntax-tree/mdast-util-phrasing
- mdast-util-to-hast 13.2.1 — MIT — https://github.com/syntax-tree/mdast-util-to-hast
- mdast-util-to-markdown 2.1.2 — MIT — https://github.com/syntax-tree/mdast-util-to-markdown
- mdast-util-to-string 4.0.0 — MIT — https://github.com/syntax-tree/mdast-util-to-string
- micromark 4.0.2 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark
- micromark-core-commonmark 2.0.3 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-core-commonmark
- micromark-extension-gfm 3.0.0 — MIT — https://github.com/micromark/micromark-extension-gfm
- micromark-extension-gfm-autolink-literal 2.1.0 — MIT — https://github.com/micromark/micromark-extension-gfm-autolink-literal
- micromark-extension-gfm-footnote 2.1.0 — MIT — https://github.com/micromark/micromark-extension-gfm-footnote
- micromark-extension-gfm-strikethrough 2.1.0 — MIT — https://github.com/micromark/micromark-extension-gfm-strikethrough
- micromark-extension-gfm-table 2.1.1 — MIT — https://github.com/micromark/micromark-extension-gfm-table
- micromark-extension-gfm-tagfilter 2.0.0 — MIT — https://github.com/micromark/micromark-extension-gfm-tagfilter
- micromark-extension-gfm-task-list-item 2.1.0 — MIT — https://github.com/micromark/micromark-extension-gfm-task-list-item
- micromark-extension-math 3.1.0 — MIT — https://github.com/micromark/micromark-extension-math
- micromark-factory-destination 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-factory-destination
- micromark-factory-label 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-factory-label
- micromark-factory-space 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-factory-space
- micromark-factory-title 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-factory-title
- micromark-factory-whitespace 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-factory-whitespace
- micromark-util-character 2.1.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-character
- micromark-util-chunked 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-chunked
- micromark-util-classify-character 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-classify-character
- micromark-util-combine-extensions 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-combine-extensions
- micromark-util-decode-numeric-character-reference 2.0.2 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-decode-numeric-character-reference
- micromark-util-decode-string 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-decode-string
- micromark-util-encode 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-encode
- micromark-util-html-tag-name 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-html-tag-name
- micromark-util-normalize-identifier 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-normalize-identifier
- micromark-util-resolve-all 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-resolve-all
- micromark-util-sanitize-uri 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-sanitize-uri
- micromark-util-subtokenize 2.1.0 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-subtokenize
- micromark-util-symbol 2.0.1 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-symbol
- micromark-util-types 2.0.2 — MIT — https://github.com/micromark/micromark/tree/main/packages/micromark-util-types
- ms 2.1.3 — MIT — https://github.com/vercel/ms
- nanoid 6.0.1 — MIT — https://github.com/ai/nanoid
- nanoid 6.0.1 — MIT — https://github.com/ai/nanoid
- nanoid 3.3.18 — MIT — https://github.com/ai/nanoid
- openapi-fetch 0.13.8 — MIT — https://openapi-ts.dev
- openapi-typescript-helpers 0.0.15 — MIT — https://openapi-ts.dev
- orderedmap 2.1.1 — MIT — https://github.com/marijnh/orderedmap
- p-limit 2.3.0 — MIT — https://github.com/sindresorhus/p-limit
- p-locate 4.1.0 — MIT — https://github.com/sindresorhus/p-locate
- p-try 2.2.0 — MIT — https://github.com/sindresorhus/p-try
- parse-entities 4.0.2 — MIT — https://github.com/wooorm/parse-entities
- path-exists 4.0.0 — MIT — https://github.com/sindresorhus/path-exists
- pngjs 5.0.0 — MIT — https://github.com/lukeapage/pngjs
- postcss 8.5.26 — MIT — https://postcss.org/
- property-information 7.2.0 — MIT — https://github.com/wooorm/property-information
- prosemirror-changeset 2.4.1 — MIT — https://code.haverbeke.berlin/prosemirror/prosemirror-changeset
- prosemirror-commands 1.7.2 — MIT — https://code.haverbeke.berlin/prosemirror/prosemirror-commands
- prosemirror-drop-indicator 0.1.4 — MIT — https://github.com/prosekit/prosemirror-drop-indicator
- prosemirror-dropcursor 1.8.3 — MIT — https://code.haverbeke.berlin/prosemirror/prosemirror-dropcursor
- prosemirror-gapcursor 1.4.1 — MIT — https://github.com/prosemirror/prosemirror-gapcursor
- prosemirror-history 1.5.0 — MIT — https://github.com/prosemirror/prosemirror-history
- prosemirror-inputrules 1.5.1 — MIT — https://github.com/prosemirror/prosemirror-inputrules
- prosemirror-keymap 1.2.3 — MIT — https://github.com/prosemirror/prosemirror-keymap
- prosemirror-model 1.25.11 — MIT — https://code.haverbeke.berlin/prosemirror/prosemirror-model
- prosemirror-safari-ime-span 1.0.2 — MIT — https://github.com/ocavue/prosemirror-safari-ime-span
- prosemirror-schema-list 1.5.1 — MIT — https://github.com/prosemirror/prosemirror-schema-list
- prosemirror-state 1.4.4 — MIT — https://github.com/prosemirror/prosemirror-state
- prosemirror-tables 1.8.5 — MIT — https://github.com/ProseMirror/prosemirror-tables
- prosemirror-transform 1.12.0 — MIT — https://github.com/prosemirror/prosemirror-transform
- prosemirror-view 1.42.2 — MIT — https://code.haverbeke.berlin/prosemirror/prosemirror-view
- prosemirror-virtual-cursor 0.4.2 — MIT — https://github.com/ocavue/prosemirror-virtual-cursor
- qrcode 1.5.4 — MIT — http://github.com/soldair/node-qrcode
- react 19.2.8 — MIT — https://react.dev/
- react-dom 19.2.8 — MIT — https://react.dev/
- react-i18next 15.7.4 — MIT — https://github.com/i18next/react-i18next
- react-markdown 10.1.0 — MIT — https://github.com/remarkjs/react-markdown
- react-remove-scroll 2.7.2 — MIT — https://github.com/theKashey/react-remove-scroll
- react-remove-scroll-bar 2.3.8 — MIT — https://github.com/theKashey/react-remove-scroll-bar (no LICENSE file in package)
- react-style-singleton 2.2.3 — MIT — https://github.com/theKashey/react-style-singleton
- rehype-highlight 7.0.2 — MIT — https://github.com/rehypejs/rehype-highlight
- remark 15.0.1 — MIT — https://remark.js.org
- remark-gfm 4.0.1 — MIT — https://github.com/remarkjs/remark-gfm
- remark-inline-links 7.0.0 — MIT — https://github.com/remarkjs/remark-inline-links
- remark-math 6.0.0 — MIT — https://github.com/remarkjs/remark-math/tree/main/packages/remark-math (no LICENSE file in package)
- remark-parse 11.0.0 — MIT — https://remark.js.org
- remark-rehype 11.1.2 — MIT — https://github.com/remarkjs/remark-rehype
- remark-stringify 11.0.0 — MIT — https://remark.js.org
- require-directory 2.1.1 — MIT — https://github.com/troygoode/node-require-directory/
- rope-sequence 1.3.4 — MIT — https://github.com/marijnh/rope-sequence
- scheduler 0.27.0 — MIT — https://react.dev/
- seroval 1.6.2 — MIT — https://github.com/lxsmnsyc/seroval/tree/main/packages/seroval
- seroval-plugins 1.6.2 — MIT — https://github.com/lxsmnsyc/seroval/tree/main/packages/plugins
- space-separated-tokens 2.0.2 — MIT — https://github.com/wooorm/space-separated-tokens
- spark-md5 3.0.2 — MIT (elected; dual WTFPL OR MIT) — git@github.com:satazor/js-spark-md5
- string-width 4.2.3 — MIT — https://github.com/sindresorhus/string-width
- stringify-entities 4.0.4 — MIT — https://github.com/wooorm/stringify-entities
- strip-ansi 6.0.1 — MIT — https://github.com/chalk/strip-ansi
- style-mod 4.1.3 — MIT — https://github.com/marijnh/style-mod
- style-to-js 1.1.21 — MIT — https://github.com/remarkablemark/style-to-js
- style-to-object 1.0.14 — MIT — https://github.com/remarkablemark/style-to-object
- tailwind-merge 3.6.0 — MIT — https://github.com/dcastil/tailwind-merge
- trim-lines 3.0.1 — MIT — https://github.com/wooorm/trim-lines
- trough 2.2.0 — MIT — https://github.com/wooorm/trough
- unified 11.0.5 — MIT — https://unifiedjs.com
- unist-util-find-after 5.0.0 — MIT — https://github.com/syntax-tree/unist-util-find-after
- unist-util-is 6.0.1 — MIT — https://github.com/syntax-tree/unist-util-is
- unist-util-position 5.0.0 — MIT — https://github.com/syntax-tree/unist-util-position
- unist-util-remove-position 5.0.0 — MIT — https://github.com/syntax-tree/unist-util-remove-position
- unist-util-stringify-position 4.0.0 — MIT — https://github.com/syntax-tree/unist-util-stringify-position
- unist-util-visit 5.1.0 — MIT — https://github.com/syntax-tree/unist-util-visit
- unist-util-visit-parents 6.0.2 — MIT — https://github.com/syntax-tree/unist-util-visit-parents
- uplot 1.6.32 — MIT — https://github.com/leeoniya/uPlot
- use-callback-ref 1.3.3 — MIT — https://github.com/theKashey/use-callback-ref/
- use-sidecar 1.1.3 — MIT — https://github.com/theKashey/use-sidecar
- use-sync-external-store 1.6.0 — MIT — https://github.com/facebook/react
- vfile 6.0.3 — MIT — https://github.com/vfile/vfile
- vfile-message 4.0.3 — MIT — https://github.com/vfile/vfile-message
- void-elements 3.1.0 — MIT — https://github.com/jadejs/void-elements
- vue 3.5.41 — MIT — https://vuejs.org/
- w3c-keyname 2.2.8 — MIT — https://github.com/marijnh/w3c-keyname
- wrap-ansi 6.2.0 — MIT — https://github.com/chalk/wrap-ansi
- yargs 15.4.1 — MIT — https://yargs.js.org/
- zwitch 2.0.4 — MIT — https://github.com/wooorm/zwitch

### BSD-3-Clause (3)

- diff 9.0.0 — BSD-3-Clause — https://github.com/kpdecker/jsdiff
- highlight.js 11.11.2 — BSD-3-Clause — https://highlightjs.org/
- source-map-js 1.2.1 — BSD-3-Clause — https://github.com/7rulnik/source-map-js

### BSD-2-Clause (1)

- entities 7.0.1 — BSD-2-Clause — https://github.com/fb55/entities

### ISC (11)

- @ungap/structured-clone 1.3.3 — ISC — https://github.com/ungap/structured-clone
- cliui 6.0.0 — ISC — http://github.com/yargs/cliui
- get-caller-file 2.0.5 — ISC — https://github.com/stefanpenner/get-caller-file
- lucide-react 0.475.0 — ISC (portions MIT, Feather) — https://lucide.dev
- picocolors 1.1.1 — ISC — https://github.com/alexeyraspopov/picocolors
- require-main-filename 2.0.0 — ISC — https://github.com/yargs/require-main-filename
- set-blocking 2.0.0 — ISC — https://github.com/yargs/set-blocking
- which-module 2.0.1 — ISC — https://github.com/nexdrew/which-module
- y18n 4.0.3 — ISC — https://github.com/yargs/y18n
- yaml 2.9.0 — ISC — https://eemeli.org/yaml/
- yargs-parser 18.1.3 — ISC — https://github.com/yargs/yargs-parser

### 0BSD (1)

- tslib 2.8.1 — 0BSD — https://www.typescriptlang.org/

### Unlicense (1)

- isbot 5.2.1 — Unlicense — https://isbot.js.org

### MPL-2.0 (1)

- @novnc/novnc 1.7.0 — MPL-2.0 — https://github.com/novnc/noVNC (core MPL-2.0; vendored pako MIT, core/des.js BSD-style)

## Companion plugins (`companion/`, Java/Gradle)

Each loader jar contains HomeHub's own code plus the shaded and relocated `nv-websocket-client` (`com.neovisionaries.ws` -> `dev.homehub.companion.shaded.ws`). Everything else below is compile-only (`compileOnly`/`modImplementation`, provided by the Minecraft server, proxy or loader at runtime) or a build tool, and is not in the jars. Licenses here come from upstream project knowledge plus the upstream repositories (Spigot-API, Velocity API source headers and nv-websocket-client POM were checked online); they were not available in a local Gradle cache.

| Library | Version | License | Scope | Notes | URL |
|---------|---------|---------|-------|-------|-----|
| com.neovisionaries:nv-websocket-client | 2.14 | Apache-2.0 | **bundled in jars** | shaded (relocated) into every loader jar | https://github.com/TakahikoKawasaki/nv-websocket-client |
| com.google.code.gson:gson | 2.8.9 | Apache-2.0 | compile-only | provided by the Minecraft server at runtime | https://github.com/google/gson |
| org.apache.logging.log4j:log4j-core | 2.17.1 | Apache-2.0 | compile-only | paper and velocity; provided by the server/proxy at runtime | https://logging.apache.org/log4j/2.x/ |
| org.spigotmc:spigot-api (Bukkit API) | 1.13.2-R0.1-SNAPSHOT | GPL-3.0 | compile-only | paper adapter (compileOnly, transitive = false); never bundled | https://hub.spigotmc.org/stash/projects/SPIGOT/repos/bukkit/browse |
| com.velocitypowered:velocity-api | 3.3.0-SNAPSHOT | MIT | compile-only | velocity adapter (compileOnly + annotationProcessor); never bundled | https://papermc.io/software/velocity |
| net.fabricmc:fabric-loader | 0.19.3 | Apache-2.0 | compile-only | fabric adapters; provided by the Fabric runtime, not in the shade configuration | https://github.com/FabricMC/fabric-loader |
| com.mojang:minecraft (+ official Mojang mappings) | 1.20.1 - 26.2 | Proprietary (Minecraft EULA) | compile-only | via Fabric Loom / ForgeGradle; never bundled or redistributed | https://www.minecraft.net/en-us/eula |
| net.minecraftforge:forge | 52.1.16 - 65.1.2 | LGPL-2.1 | compile-only | forge adapters (forge/target.gradle `implementation`); not in the `shade` configuration | https://github.com/MinecraftForge/MinecraftForge |
| SpongePowered Mixin (via fabric-loader) | provided | MIT | compile-only | referenced by fabric/mixin/*.java; provided at runtime | https://github.com/SpongePowered/Mixin |
| com.mojang:brigadier (via Minecraft) | provided | MIT | compile-only | referenced from loader sources; provided by Minecraft | https://github.com/Mojang/brigadier |
| org.slf4j:slf4j-api (via Minecraft/Velocity) | provided | MIT | compile-only | referenced from loader sources; provided at runtime | https://www.slf4j.org/ |
| it.unimi.dsi:fastutil (via Minecraft) | provided | Apache-2.0 | compile-only | referenced from loader sources; provided by Minecraft | https://fastutil.di.unimi.it/ |
| com.gradleup.shadow (Gradle plugin) | 9.6.1 | Apache-2.0 | build tool | Gradle plugin/tooling; not distributed | https://github.com/GradleUp/shadow |
| net.fabricmc.fabric-loom / fabric-loom-remap (Gradle plugin) | 1.17.19 | MIT | build tool | Gradle plugin/tooling; not distributed | https://github.com/FabricMC/fabric-loom |
| net.minecraftforge.gradle (ForgeGradle 7) | 7.0.3+ / 7.0.17+ | LGPL-2.1 | build tool | Gradle plugin/tooling; not distributed | https://github.com/MinecraftForge/ForgeGradle |
| org.gradle.toolchains.foojay-resolver-convention | 1.0.0 | Apache-2.0 | build tool | Gradle plugin/tooling; not distributed | https://github.com/gradle/foojay-toolchains |
| Gradle (wrapper 9.5.1; Dockerfile uses 8.10.2) | 9.5.1 | Apache-2.0 | build tool | Gradle plugin/tooling; not distributed | https://github.com/gradle/gradle |

Jar targets: Paper/Spigot (one jar, spigot-api 1.13.2 + reflection), Velocity 3.3+, Fabric (9 jars, MC 1.20.1 - 26.2) and Forge (8 jars, MC 1.21.1 - 26.2).

## Bundled data, fonts and assets

| Item | Version | License | Where | URL |
|------|---------|---------|-------|-----|
| IEEE OUI registry (MA-L/MA-M/MA-S) as internal/netscan/data/oui.tsv | n/a (compacted snapshot) | Public registry data (IEEE Registration Authority) | embedded (go:embed) in the server binary | https://standards-oui.ieee.org/ |
| Inter Variable (web/src/assets/fonts/InterVariable.woff2) | 4.001 | OFL-1.1 | bundled in the web UI | https://github.com/rsms/inter |
| KaTeX fonts (KaTeX_*.woff2/woff/ttf, from the katex package) | 0.18.4 | OFL-1.1 | bundled in web/dist via @milkdown/crepe (math) | https://github.com/KaTeX/KaTeX |
| Minecraft default "Steve" skin (internal/integrations/minecraft/steve.png) | n/a | Proprietary (Mojang Studios / Microsoft) | embedded (go:embed) in the server binary - SEE NEEDS ATTENTION | https://www.minecraft.net/en-us/eula |
| web/public/favicon.svg and web/src/assets (other) | n/a | First-party | no other third-party assets found | - |

- **IEEE OUI registry**: `internal/netscan/data/oui.tsv` is the public IEEE Registration Authority assignment list (52,995 MA-L/MA-M/MA-S entries compacted to `ASSIGNMENT<TAB>vendor`). The IEEE publishes it for public download without an explicit license; it is factual registry data. Vendor names are trademarks of their owners. Source: https://standards-oui.ieee.org/
- **Inter**: Copyright 2016 The Inter Project Authors (https://github.com/rsms/inter); "Inter UI and Inter is a trademark of rsms". Licensed under the SIL Open Font License 1.1 (text below). The font is embedded unmodified and is not sold by itself.
- **KaTeX fonts** (KaTeX_AMS, Caligraphic, Fraktur, Main, Math, SansSerif, Script, Size1-4, Typewriter): Copyright (c) 2009-2010 Design Science, Inc. (www.mathjax.org); Copyright (c) 2014-2018 Khan Academy (www.khanacademy.org), with Reserved Font Name "KaTeX_*". Licensed under the SIL OFL 1.1 as stated in each font file's name table. The KaTeX JavaScript/CSS is MIT (listed with the npm packages).
- **Not shipped**: Minecraft item/GUI textures used by the inventory viewer are downloaded by the operator from Mojang's servers at runtime (`homehub minecraft-textures`) and never redistributed; the only Mojang asset in the repo is the Steve skin from Needs attention item 1.
- **First-party**: `web/public/favicon.svg`, all other files under `web/src/assets` and `web/public`.

## License texts

Each distinct license text appears once; the packages that use it and the copyright notices from their LICENSE files follow it. Texts that differ from the reference text only in whitespace, quote style or one clause are listed under the reference variant.

### Apache License 2.0 (Apache-2.0)

Used by (35): dompurify, github.com/distribution/reference, github.com/docker/docker, github.com/docker/go-connections, github.com/docker/go-units, github.com/go-logr/logr, github.com/go-logr/stdr, github.com/go-openapi/jsonpointer, github.com/go-openapi/swag/jsonname, github.com/google/go-tpm, github.com/klauspost/compress, github.com/minio/crc64nvme, github.com/minio/md5-simd, github.com/minio/minio-go/v7, github.com/moby/docker-image-spec, github.com/oapi-codegen/runtime, github.com/oasdiff/yaml3, github.com/opencontainers/go-digest, github.com/opencontainers/image-spec, github.com/pquerna/otp, github.com/richardlehane/mscfb, github.com/richardlehane/msoleps, github.com/santhosh-tekuri/jsonschema/v6, github.com/sethvargo/go-retry, github.com/xdg-go/scram, github.com/xdg-go/stringprep, go.mongodb.org/mongo-driver/v2, go.opentelemetry.io/auto/sdk, go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp, go.opentelemetry.io/otel, go.opentelemetry.io/otel/metric, go.opentelemetry.io/otel/trace, go.yaml.in/yaml/v3, gopkg.in/ini.v1, typescript, plus the companion libraries marked Apache-2.0 above (nv-websocket-client, gson, log4j-core, fabric-loader). Copyright holders are named by the individual projects; where a project ships a NOTICE file it is reproduced after the license text.

```text
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright [yyyy] [name of copyright owner]

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
```

#### Apache NOTICE files

**github.com/docker/docker**

```text
Docker
Copyright 2012-2017 Docker, Inc.

This product includes software developed at Docker, Inc. (https://www.docker.com).

This product contains software (https://github.com/creack/pty) developed
by Keith Rarick, licensed under the MIT License.

The following is courtesy of our legal counsel:


Use and transfer of Docker may be subject to certain restrictions by the
United States and other governments.
It is your responsibility to ensure that your use and/or transfer does not
violate applicable laws.

For more information, please see https://www.bis.doc.gov

See also https://www.apache.org/dev/crypto.html and/or seek legal counsel.
```

**github.com/go-openapi/jsonpointer**

```text
Copyright 2015-2025 go-swagger maintainers

// SPDX-FileCopyrightText: Copyright 2015-2025 go-swagger maintainers
// SPDX-License-Identifier: Apache-2.0

This software library, github.com/go-openapi/jsonpointer, includes software developed
by the go-swagger and go-openapi maintainers ("go-swagger maintainers").

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this software except in compliance with the License.

You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0.

This software is copied from, derived from, and inspired by other original software products.
It ships with copies of other software which license terms are recalled below.

The original software was authored on 25-02-2013 by sigu-399 (https://github.com/sigu-399, sigu.399@gmail.com).

github.com/sigu-399/jsonpointer
===========================

// SPDX-FileCopyrightText: Copyright 2013 sigu-399 ( https://github.com/sigu-399 )
// SPDX-License-Identifier: Apache-2.0

Copyright 2013 sigu-399 ( https://github.com/sigu-399 )

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

**github.com/minio/minio-go/v7**

```text
MinIO Cloud Storage, (C) 2014-2020 MinIO, Inc.

This product includes software developed at MinIO, Inc.
(https://min.io/).

The MinIO project contains unmodified/modified subcomponents too with
separate copyright notices and license terms. Your use of the source
code for these subcomponents is subject to the terms and conditions
of Apache License Version 2.0
```

**github.com/oasdiff/yaml3**

```text
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

**github.com/pquerna/otp**

```text
otp
Copyright (c) 2014, Paul Querna

This product includes software developed by 
Paul Querna (http://paul.querna.org/).
```

**go.yaml.in/yaml/v3**

```text
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

Individual Apache-2.0 LICENSE files that carry their own copyright line: github.com/docker/docker: Copyright 2013-2018 Docker, Inc.; github.com/docker/go-connections: Copyright 2015 Docker, Inc.; github.com/docker/go-units: Copyright 2015 Docker, Inc.; github.com/klauspost/compress: Copyright (c) 2012 The Go Authors; Copyright (c) 2019 Klaus Post (BSD-3-Clause, main) / Copyright (c) 2011 The Snappy-Go Authors (BSD-3-Clause; s2, snappy, internal/snapref) / Copyright (c) 2016 Caleb Spare (MIT; zstd/internal/xxhash); github.com/oasdiff/yaml3: Copyright (c) 2006-2011 Kirill Simonov (MIT; libyaml port) / Copyright (c) 2011-2019 Canonical Ltd (Apache-2.0); github.com/opencontainers/go-digest: Copyright 2019, 2020 OCI Contributors / Copyright 2016 Docker, Inc.; github.com/opencontainers/image-spec: Copyright 2016 The Linux Foundation.; go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp: Copyright 2009 The Go Authors (BSD-3-Clause portion); go.opentelemetry.io/otel: Copyright 2009 The Go Authors (BSD-3-Clause portion); go.opentelemetry.io/otel/metric: Copyright 2009 The Go Authors (BSD-3-Clause portion); go.opentelemetry.io/otel/trace: Copyright 2009 The Go Authors (BSD-3-Clause portion); go.yaml.in/yaml/v3: Copyright (c) 2006-2011 Kirill Simonov (MIT; libyaml port) / Copyright (c) 2011-2019 Canonical Ltd (Apache-2.0); gopkg.in/ini.v1: Copyright 2014 Unknwon.

### MIT License (MIT)

Used by 377 packages (Go and npm; includes the MIT portions of compound licenses). Several packages word the grant slightly differently (single quotes instead of double quotes, "(including the next paragraph)"); the terms are the same as the reference text. Copyright notices follow the text, grouped by identical notice.

```text
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

#### MIT copyright notices

- github.com/apapsch/go-jsonmerge/v2 — Copyright (c) 2016-2019 Artur Kraev
- github.com/boombuler/barcode — Copyright (c) 2014 Florian Sundermann
- github.com/cespare/xxhash/v2 — Copyright (c) 2016 Caleb Spare
- github.com/dustin/go-humanize — Copyright (c) 2005-2008 Dustin Sallings <dustin@spy.net>
- github.com/emersion/go-ical, github.com/emersion/go-webdav — Copyright (c) 2020 Simon Ser
- github.com/felixge/httpsnoop — Copyright (c) 2016 Felix Geisendörfer (felix@debuggable.com)
- github.com/fxamacker/cbor/v2 — Copyright (c) 2019-present Faye Amacker
- github.com/gabriel-vasile/mimetype — Copyright (c) 2018 Gabriel Vasile
- github.com/getkin/kin-openapi — Copyright (c) 2017-2018 the project authors.
- github.com/go-chi/chi/v5 — Copyright (c) 2015-present Peter Kieltyka (https://github.com/pkieltyka), Google Inc.
- github.com/go-viper/mapstructure/v2 — Copyright (c) 2013 Mitchell Hashimoto
- github.com/golang-jwt/jwt/v5 — Copyright (c) 2012 Dave Grijalva / Copyright (c) 2021 golang-jwt maintainers
- github.com/jackc/pgpassfile — Copyright (c) 2019 Jack Christensen
- github.com/jackc/pgservicefile — Copyright (c) 2020 Jack Christensen
- github.com/jackc/pgx/v5 — Copyright (c) 2013-2021 Jack Christensen
- github.com/jackc/puddle/v2 — Copyright (c) 2018 Jack Christensen
- github.com/klauspost/compress — Copyright (c) 2012 The Go Authors; Copyright (c) 2019 Klaus Post (BSD-3-Clause, main) / Copyright (c) 2011 The Snappy-Go Authors (BSD-3-Clause; s2, snappy, internal/snapref) / Copyright (c) 2016 Caleb Spare (MIT; zstd/internal/xxhash)
- github.com/klauspost/cpuid/v2 — Copyright (c) 2015 Klaus Post
- github.com/mfridman/interpolate — Copyright (c) 2014-2017 Buildkite Pty Ltd / Copyright (c) 2023 Michael Fridman
- github.com/Microsoft/go-winio — Copyright (c) 2015 Microsoft
- github.com/oasdiff/yaml — Copyright (c) 2014 Sam Ghods (MIT) / Copyright (c) 2012 The Go Authors (BSD-3-Clause portion)
- github.com/oasdiff/yaml3, go.yaml.in/yaml/v3 — Copyright (c) 2006-2011 Kirill Simonov (MIT; libyaml port) / Copyright (c) 2011-2019 Canonical Ltd (Apache-2.0)
- github.com/philhofer/fwd — Copyright (c) 2014-2015, Philip Hofer
- github.com/pressly/goose/v3 — Original work Copyright (c) 2012 Liam Staskawicz / Modified work Copyright (c) 2016 Vojtech Vitek / Modified work Copyright (c) 2021 Michael Fridman, Vojtech Vitek
- github.com/robfig/cron/v3 — Copyright (C) 2012 Rob Figueiredo
- github.com/rs/xid — Copyright (c) 2015 Olivier Poitrey <rs@dailymotion.com>
- github.com/teambition/rrule-go — Copyright (c) 2017-2023 Teambition
- github.com/tiendc/go-deepcopy — Copyright (c) 2023 tiendc
- github.com/tinylib/msgp — Copyright (c) 2014 Philip Hofer / Portions Copyright (c) 2009 The Go Authors
- github.com/x448/float16 — Copyright (c) 2019 Montgomery Edwards⁴⁴⁸ and Faye Amacker
- github.com/youmark/pkcs8 — Copyright (c) 2014 youmark
- go.uber.org/multierr — Copyright (c) 2017-2021 Uber Technologies, Inc.
- @babel/helper-string-parser, @babel/helper-validator-identifier, @babel/runtime, @babel/types — Copyright (c) 2014-present Sebastian McKenzie and other contributors
- @babel/parser — Copyright (C) 2012-2014 by various contributors (see AUTHORS)
- @codemirror/autocomplete, @codemirror/commands, @codemirror/lang-angular, @codemirror/lang-cpp, @codemirror/lang-css, @codemirror/lang-html, @codemirror/lang-java, @codemirror/lang-javascript, @codemirror/lang-json, @codemirror/lang-less, @codemirror/lang-markdown, @codemirror/lang-php, @codemirror/lang-python, @codemirror/lang-rust, @codemirror/lang-sass, @codemirror/lang-sql, @codemirror/lang-vue, @codemirror/lang-wast, @codemirror/lang-xml, @codemirror/language, @codemirror/language-data, @codemirror/legacy-modes, @codemirror/lint, @codemirror/search, @codemirror/state, @codemirror/theme-one-dark, @codemirror/view, codemirror — Copyright (C) 2018-2021 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @codemirror/lang-go, @codemirror/lang-yaml — Copyright (C) 2024 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @codemirror/lang-jinja — Copyright (C) 2025 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @codemirror/lang-liquid — Copyright (C) 2018-2023 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @floating-ui/core, @floating-ui/dom, @floating-ui/react-dom, @floating-ui/utils — Copyright (c) 2021-present Floating UI contributors
- @jridgewell/sourcemap-codec — Copyright 2024 Justin Ridgewell <justin@ridgewell.name>
- @lezer/common, @lezer/css, @lezer/highlight, @lezer/html, @lezer/javascript, @lezer/lr, @lezer/php, @lezer/rust, @lezer/sass, @lezer/xml, style-mod — Copyright (C) 2018 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @lezer/cpp, @lezer/go, @lezer/java, @lezer/markdown, @lezer/python — Copyright (C) 2020 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- @lezer/json — Copyright (C) 2020 by Marijn Haverbeke <marijn@haverbeke.berlin>, Arun Srinivasan <rulfzid@gmail.com>, and others
- @lezer/yaml — Copyright (C) 2024 by Marijn Haverbeke <marijnh@gmail.com> and others
- @marijn/find-cluster-break — Copyright (C) 2024 by Marijn Haverbeke <marijn@haverbeke.berlin>
- @milkdown/components, @milkdown/core, @milkdown/crepe, @milkdown/ctx, @milkdown/exception, @milkdown/kit, @milkdown/plugin-block, @milkdown/plugin-clipboard, @milkdown/plugin-cursor, @milkdown/plugin-diff, @milkdown/plugin-history, @milkdown/plugin-indent, @milkdown/plugin-listener, @milkdown/plugin-slash, @milkdown/plugin-streaming, @milkdown/plugin-tooltip, @milkdown/plugin-trailing, @milkdown/plugin-upload, @milkdown/preset-commonmark, @milkdown/preset-gfm, @milkdown/prose, @milkdown/transformer, @milkdown/utils — Copyright (c) 2020-present Mirone
- @ocavue/utils, prosemirror-drop-indicator — Copyright (c) 2025 ocavue
- @radix-ui/number, @radix-ui/primitive, @radix-ui/react-arrow, @radix-ui/react-checkbox, @radix-ui/react-collection, @radix-ui/react-compose-refs, @radix-ui/react-context, @radix-ui/react-dialog, @radix-ui/react-direction, @radix-ui/react-dismissable-layer, @radix-ui/react-dropdown-menu, @radix-ui/react-focus-guards, @radix-ui/react-focus-scope, @radix-ui/react-id, @radix-ui/react-menu, @radix-ui/react-popover, @radix-ui/react-popper, @radix-ui/react-portal, @radix-ui/react-presence, @radix-ui/react-primitive, @radix-ui/react-roving-focus, @radix-ui/react-select, @radix-ui/react-slot, @radix-ui/react-switch, @radix-ui/react-tabs, @radix-ui/react-tooltip, @radix-ui/react-use-callback-ref, @radix-ui/react-use-controllable-state, @radix-ui/react-use-effect-event, @radix-ui/react-use-is-hydrated, @radix-ui/react-use-layout-effect, @radix-ui/react-use-previous, @radix-ui/react-use-rect, @radix-ui/react-use-size, @radix-ui/react-visually-hidden, @radix-ui/rect — Copyright (c) 2022 WorkOS
- @simplewebauthn/browser — Copyright (c) 2020 Matthew Miller
- @tanstack/history, @tanstack/query-core, @tanstack/react-query, @tanstack/react-router, @tanstack/router-core — Copyright (c) 2021-present Tanner Linsley
- @tanstack/react-store, @tanstack/store — Copyright (c) 2021 Tanner Linsley
- @types/debug, @types/estree, @types/estree-jsx, @types/hast, @types/katex, @types/lodash, @types/lodash-es, @types/mdast, @types/ms, @types/react, @types/react-dom, @types/trusted-types, @types/unist, @types/unist — Copyright (c) Microsoft Corporation.
- @vue/compiler-core, @vue/compiler-dom, @vue/compiler-sfc, @vue/compiler-ssr, @vue/reactivity, @vue/runtime-core, @vue/runtime-dom, @vue/server-renderer, @vue/shared, vue — Copyright (c) 2018-present, Yuxi (Evan) You
- @xterm/addon-fit — Copyright (c) 2019, The xterm.js authors (https://github.com/xtermjs/xterm.js)
- @xterm/addon-search — Copyright (c) 2017, The xterm.js authors (https://github.com/xtermjs/xterm.js)
- @xterm/xterm — Copyright (c) 2017-2019, The xterm.js authors (https://github.com/xtermjs/xterm.js) / Copyright (c) 2014-2016, SourceLair Private Company (https://www.sourcelair.com) / Copyright (c) 2012-2013, Christopher Jeffrey (https://github.com/chjj/)
- ansi-regex, ansi-styles, camelcase, decamelize, find-up, is-fullwidth-code-point, locate-path, p-limit, p-locate, p-try, path-exists, string-width, strip-ansi, wrap-ansi — Copyright (c) Sindre Sorhus <sindresorhus@gmail.com> (sindresorhus.com)
- aria-hidden, react-remove-scroll, react-style-singleton, use-callback-ref, use-sidecar — Copyright (c) 2017 Anton Korzunov
- bail, ccount, character-entities, character-entities-html4, character-entities-legacy, character-reference-invalid, mdast-util-to-string, remark-inline-links, unified, unist-util-find-after, unist-util-is, unist-util-position, unist-util-visit, vfile — Copyright (c) 2015 Titus Wormer <tituswormer@gmail.com>
- clsx, dequal — Copyright (c) Luke Edwards <luke.edwards05@gmail.com> (lukeed.com)
- cmdk — Copyright (c) 2022 Paco Coursey
- color-convert — Copyright (c) 2011-2016 Heather Arthur <fayearthur@gmail.com>
- color-name — Copyright (c) 2015 Dmitry Ivanov
- comma-separated-tokens, hast-util-is-element, hast-util-whitespace, is-alphabetical, is-alphanumerical, is-decimal, is-hexadecimal, mdast-util-to-hast, space-separated-tokens, trough, unist-util-remove-position, unist-util-stringify-position, unist-util-visit-parents, zwitch — Copyright (c) 2016 Titus Wormer <tituswormer@gmail.com>
- commander — Copyright (c) 2011 TJ Holowaychuk <tj@vision-media.ca>
- cookie-es — Cookie-es copyright (c) Pooya Parsa <pooya@pi0.io> / Copyright (c) 2012-2014 Roman Shtylman <shtylman@gmail.com> / Copyright (c) 2015 Douglas Christopher Wilson <doug@somethingdoug.com> / Copyright (c) 2015 Nathan Friedly <nathan@nfriedly.com> (http://nfriedly.com/)
- crelt — Copyright (C) 2020 by Marijn Haverbeke <marijn@haverbeke.berlin>
- cron-parser — Copyright (c) 2014-2023 Harri Siirak
- cronstrue — Copyright (c) 2017 Brady Holt
- csstype — Copyright (c) 2017-2018 Fredrik Nicol
- debug — Copyright (c) 2014-2017 TJ Holowaychuk <tj@vision-media.ca> / Copyright (c) 2018-2021 Josh Junon
- decode-named-character-reference, hast-util-to-jsx-runtime, lowlight, markdown-table, mdast-util-find-and-replace, mdast-util-from-markdown, mdast-util-gfm, mdast-util-gfm-footnote, mdast-util-to-markdown, micromark, micromark-core-commonmark, micromark-extension-gfm-table, micromark-factory-destination, micromark-factory-label, micromark-factory-space, micromark-factory-title, micromark-factory-whitespace, micromark-util-character, micromark-util-chunked, micromark-util-classify-character, micromark-util-combine-extensions, micromark-util-decode-numeric-character-reference, micromark-util-decode-string, micromark-util-encode, micromark-util-html-tag-name, micromark-util-normalize-identifier, micromark-util-resolve-all, micromark-util-sanitize-uri, micromark-util-subtokenize, micromark-util-symbol, micromark-util-types, rehype-highlight, remark-gfm, remark-rehype, vfile-message — Copyright (c) Titus Wormer <tituswormer@gmail.com>
- detect-node-es — Copyright (c) 2017 Ilya Kantor
- devlop — Copyright (c) 2023 Titus Wormer <tituswormer@gmail.com>
- emoji-regex — Copyright Mathias Bynens <https://mathiasbynens.be/>
- escape-string-regexp, is-plain-obj — Copyright (c) Sindre Sorhus <sindresorhus@gmail.com> (https://sindresorhus.com)
- estree-util-is-identifier-name, mdast-util-gfm-autolink-literal, mdast-util-gfm-strikethrough, mdast-util-gfm-table, mdast-util-gfm-task-list-item, mdast-util-math, mdast-util-mdx-expression, mdast-util-mdx-jsx, mdast-util-mdxjs-esm, micromark-extension-gfm, micromark-extension-gfm-autolink-literal, micromark-extension-gfm-strikethrough, micromark-extension-gfm-tagfilter, micromark-extension-gfm-task-list-item, micromark-extension-math — Copyright (c) 2020 Titus Wormer <tituswormer@gmail.com>
- estree-walker — Copyright (c) 2015-20 [these people](https://github.com/Rich-Harris/estree-walker/graphs/contributors)
- exifr — Copyright (c) 2020 Mike Kovařík, Mutiny.cz
- extend — Copyright (c) 2014 Stefan Thomas
- get-nonce — Copyright (c) 2020 Anton Korzunov
- hast-util-to-text — Copyright (c) 2019 Titus Wormer <tituswormer@gmail.com>
- html-parse-stringify — Copyright (c) 2025 Henrik Joreteg <henrik@joreteg.com>
- html-url-attributes — Copyright (c) Titus Wormer
- i18next, react-i18next — Copyright (c) 2025 i18next
- inline-style-parser — Copyright (c) 2012 TJ Holowaychuk <tj@vision-media.ca>
- js-sha1, js-sha512 — Copyright 2014-2024 Chen, Yi-Cyuan
- js-sha256 — Copyright (c) 2014-2026 Chen, Yi-Cyuan
- katex, katex — Copyright (c) 2013-2020 Khan Academy and other contributors
- lodash-es — Copyright OpenJS Foundation and other contributors <https://openjsf.org/> / Based on Underscore.js, copyright Jeremy Ashkenas,
- longest-streak, stringify-entities, trim-lines — Copyright (c) 2015 Titus Wormer <mailto:tituswormer@gmail.com>
- luxon — Copyright 2019 JS Foundation and other contributors
- magic-string — Copyright 2018 Rich Harris
- mdast-util-definitions — Copyright (c) 2015-2016 Titus Wormer <tituswormer@gmail.com>
- mdast-util-phrasing — Copyright (c) 2017 Titus Wormer <tituswormer@gmail.com> / Copyright (c) 2017 Victor Felder <victor@draft.li>
- micromark-extension-gfm-footnote — Copyright (c) 2021 Titus Wormer <tituswormer@gmail.com>
- ms — Copyright (c) 2020 Vercel, Inc.
- nanoid, nanoid — Copyright 2017 Andrey Sitnik <andrey@sitnik.es>
- nanoid — Copyright 2017 Andrey Sitnik <andrey@sitnik.ru>
- openapi-fetch, openapi-typescript-helpers — Copyright (c) 2023 Drew Powers
- orderedmap, w3c-keyname — Copyright (C) 2016 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- parse-entities, property-information — Copyright (c) Titus Wormer <mailto:tituswormer@gmail.com>
- pngjs — pngjs2 original work Copyright (c) 2015 Luke Page & Original Contributors / pngjs derived work Copyright (c) 2012 Kuba Niegowski
- postcss — Copyright 2013 Andrey Sitnik <andrey@sitnik.es>
- prosemirror-changeset — Copyright (C) 2017 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- prosemirror-commands, prosemirror-dropcursor, prosemirror-gapcursor, prosemirror-history, prosemirror-inputrules, prosemirror-keymap, prosemirror-model, prosemirror-schema-list, prosemirror-state, prosemirror-transform, prosemirror-view — Copyright (C) 2015-2017 by Marijn Haverbeke <marijn@haverbeke.berlin> and others
- prosemirror-safari-ime-span — Copyright (c) 2024 ocavue
- prosemirror-tables — Copyright (C) 2015-2016 by Marijn Haverbeke <marijnh@gmail.com> and others
- prosemirror-virtual-cursor — Copyright (c) 2022 ocavue
- qrcode — Copyright (c) 2012 Ryan Day
- react, react-dom, scheduler, use-sync-external-store — Copyright (c) Meta Platforms, Inc. and affiliates.
- react-markdown — Copyright (c) Espen Hovlandsdal
- react-remove-scroll-bar — (no LICENSE file shipped; package.json author: Anton Korzunov <thekashey@gmail.com>)
- remark, remark-parse, remark-stringify — Copyright (c) 2014 Titus Wormer <tituswormer@gmail.com>
- remark-math — (no LICENSE file shipped; package.json author: Junyoung Choi <fluke8259@gmail.com> (https://rokt33r.github.io))
- require-directory — Copyright (c) 2011 Troy Goode <troygoode@gmail.com>
- rope-sequence — Copyright (C) 2016 by Marijn Haverbeke <marijn@haverbeke.berlin>
- seroval, seroval-plugins — MIT License Copyright (c) 2025 Alexis Munsayac <alexis.munsayac@gmail.com>
- style-to-js — Copyright (c) 2020 Menglin "Mark" Xu <mark@remarkablemark.org>
- style-to-object — Copyright (c) 2017 Menglin "Mark" Xu <mark@remarkablemark.org>
- tailwind-merge — Copyright (c) 2021 Dany Castillo
- uplot — Copyright (c) 2022 Leon Sorokin
- void-elements — Copyright (c) 2014 hemanth
- yargs — Copyright 2010 James Halliday (mail@substack.net); Modified work Copyright 2014 Contributors (ben@npmjs.com)

Other notices shipped by MIT packages: `lodash-es` also states that sample code in its documentation is waived under CC0 and that files in its node_modules/vendor directories carry their own licenses; `dijkstrajs` ships the abbreviated notice reproduced below.

`dijkstrajs` LICENSE (declared MIT):

```text
Dijkstra path-finding functions. Adapted from the Dijkstar Python project.

Copyright (C) 2008
  Wyatt Baldwin <self@wyattbaldwin.com>
  All rights reserved

Licensed under the MIT license.

  http://www.opensource.org/licenses/mit-license.php

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

### BSD licenses (BSD-3-Clause and BSD-2-Clause)

11 distinct texts, because the non-endorsement clause and wording differ per project. Each text is followed by its users and their copyright notices.

#### BSD text 1: BSD-3-Clause - non-endorsement clause names: Google LLC

Used by: filippo.io/hpke, github.com/klauspost/compress, github.com/oasdiff/yaml, go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp, go.opentelemetry.io/otel, go.opentelemetry.io/otel/metric, go.opentelemetry.io/otel/trace, golang.org/x/crypto, golang.org/x/net, golang.org/x/sync, golang.org/x/sys, golang.org/x/text, golang.org/x/time

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- filippo.io/hpke, golang.org/x/crypto, golang.org/x/net, golang.org/x/sync, golang.org/x/sys, golang.org/x/text, golang.org/x/time — Copyright 2009 The Go Authors.
- github.com/klauspost/compress — Copyright (c) 2012 The Go Authors; Copyright (c) 2019 Klaus Post (BSD-3-Clause, main) / Copyright (c) 2011 The Snappy-Go Authors (BSD-3-Clause; s2, snappy, internal/snapref) / Copyright (c) 2016 Caleb Spare (MIT; zstd/internal/xxhash)
- github.com/oasdiff/yaml — Copyright (c) 2014 Sam Ghods (MIT) / Copyright (c) 2012 The Go Authors (BSD-3-Clause portion)
- go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp, go.opentelemetry.io/otel, go.opentelemetry.io/otel/metric, go.opentelemetry.io/otel/trace — Copyright 2009 The Go Authors (BSD-3-Clause portion)

#### BSD text 2: BSD-3-Clause - non-endorsement clause names: Google Inc.

Used by: filippo.io/edwards25519, github.com/geoffgarside/ber, github.com/gogo/protobuf, github.com/google/uuid, github.com/klauspost/crc32

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- filippo.io/edwards25519, github.com/geoffgarside/ber — Copyright (c) 2009 The Go Authors. All rights reserved.
- github.com/gogo/protobuf — Copyright (c) 2013, The GoGo Authors / Copyright 2010 The Go Authors
- github.com/google/uuid — Copyright (c) 2009,2014 Google Inc. All rights reserved.
- github.com/klauspost/crc32 — Copyright (c) 2012 The Go Authors. All rights reserved.

#### BSD text 3: BSD-3-Clause - non-endorsement clause names: the copyright holder

Used by: diff, github.com/go-webauthn/webauthn, github.com/go-webauthn/x, github.com/miekg/dns

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions
are met:

1. Redistributions of source code must retain the above copyright
   notice, this list of conditions and the following disclaimer.
2. Redistributions in binary form must reproduce the above copyright
   notice, this list of conditions and the following disclaimer in the
   documentation and/or other materials provided with the distribution.
3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS
IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR
CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL,
EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR
PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF
LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING
NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/go-webauthn/webauthn — Copyright (c) 2025 github.com/go-webauthn/webauthn authors.
- github.com/go-webauthn/x — Copyright (c) 2021-2023 github.com/go-webauthn authors. / Copyright (c) 2014 CloudFlare Inc. (BSD-2-Clause; revoke/)
- github.com/miekg/dns — Copyright (c) 2009, The Go Authors. Extensions copyright (c) 2011, Miek Gieben.
- diff — Copyright (c) 2009-2015, Kevin Decker <kpdecker@gmail.com>

#### BSD text 4: BSD-3-Clause - non-endorsement clause names: the copyright holder

Used by: github.com/xuri/excelize/v2, highlight.js

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

* Neither the name of the copyright holder nor the names of its
  contributors may be used to endorse or promote products derived from
  this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/xuri/excelize/v2 — Copyright (c) 2016-2026 The excelize Authors. / Copyright (c) 2011-2017 Geoffrey J. Teale
- highlight.js — Copyright (c) 2006, Ivan Sagalaev.

#### BSD text 5: BSD-3-Clause - non-endorsement clause names: the age project

Used by: filippo.io/age

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of the age project nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- filippo.io/age — Copyright 2019 The age Authors / Copyright 2019 Google LLC / Copyright 2022 Filippo Valsorda

#### BSD text 6: BSD-3-Clause - non-endorsement clause names: efp

Used by: github.com/xuri/efp

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

* Neither the name of efp nor the names of its
  contributors may be used to endorse or promote products derived from
  this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/xuri/efp — Copyright (c) 2017 - 2025 Ri Xu All rights reserved.

#### BSD text 7: BSD-3-Clause - non-endorsement clause names: nfp

Used by: github.com/xuri/nfp

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

* Neither the name of nfp nor the names of its
  contributors may be used to endorse or promote products derived from
  this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/xuri/nfp — Copyright (c) 2022-2025 Ri Xu All rights reserved.

#### BSD text 8: BSD-3-Clause

Used by: source-map-js

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

* Neither the names of the Mozilla Foundation nor the names of project
  contributors may be used to endorse or promote products derived from this
  software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- source-map-js — Copyright (c) 2009-2011, Mozilla Foundation and contributors

#### BSD text 9: BSD-2-Clause

Used by: github.com/go-webauthn/x, github.com/pkg/errors, github.com/vmware/go-nfs-client, github.com/zeebo/xxh3, github.com/go-webauthn/x (revoke/ subpackage)

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice, this
  list of conditions and the following disclaimer.

* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/go-webauthn/x — Copyright (c) 2021-2023 github.com/go-webauthn authors. / Copyright (c) 2014 CloudFlare Inc. (BSD-2-Clause; revoke/)
- github.com/pkg/errors — Copyright (c) 2015, Dave Cheney <dave@cheney.net>
- github.com/vmware/go-nfs-client — Copyright � 2017 VMware, Inc. All rights reserved
- github.com/zeebo/xxh3 — Copyright (c) 2012-2014, Yann Collet / Copyright (c) 2019, Jeff Wendling
- github.com/go-webauthn/x (revoke/ subpackage) — Copyright (c) 2014 CloudFlare Inc.

#### BSD text 10: BSD-2-Clause

Used by: github.com/hirochachacha/go-smb2

```text
Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- github.com/hirochachacha/go-smb2 — Copyright (c) 2016 Hiroshi Ioka. All rights reserved.

#### BSD text 11: BSD-2-Clause

Used by: entities

```text
Redistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:

Redistributions of source code must retain the above copyright notice, this list of conditions and the following disclaimer.

Redistributions in binary form must reproduce the above copyright notice, this list of conditions and the following disclaimer in the documentation and/or other materials provided with the distribution.

THIS IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS,
EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

Copyright notices:

- entities — Copyright (c) Felix Böhm

### ISC License

Used by: github.com/coder/websocket, github.com/rasky/go-xdr

```text
Permission to use, copy, modify, and distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
```

Copyright notices:

- github.com/coder/websocket — Copyright (c) 2025 Coder
- github.com/rasky/go-xdr — Copyright (c) 2012-2014 Dave Collins <dave@davec.name>

Used by: @ungap/structured-clone, cliui, get-caller-file, lucide-react, picocolors, require-main-filename, set-blocking, which-module, y18n, yaml, yargs-parser

```text
Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE
OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

Copyright notices:

- @ungap/structured-clone — Copyright (c) 2021, Andrea Giammarchi, @WebReflection
- cliui, y18n — Copyright (c) 2015, Contributors
- get-caller-file — Copyright 2018 Stefan Penner
- lucide-react — Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as part of Feather (MIT). All other copyright (c) for Lucide are held by Lucide Contributors 2022.
- picocolors — Copyright (c) 2021-2024 Oleksii Raspopov, Kostiantyn Denysov, Anton Verinov
- require-main-filename, set-blocking, which-module, yargs-parser — Copyright (c) 2016, Contributors
- yaml — Copyright Eemeli Aro <eemeli@gmail.com>

### 0BSD (tslib)

```text
Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.
```

Copyright: Copyright (c) Microsoft Corporation.

### Unlicense (isbot)

```text
# Unlicense

This is free and unencumbered software released into the public domain.

Anyone is free to copy, modify, publish, use, compile, sell, or
distribute this software, either in source code form or as a compiled
binary, for any purpose, commercial or non-commercial, and by any
means.

In jurisdictions that recognize copyright laws, the author or authors
of this software dedicate any and all copyright interest in the
software to the public domain. We make this dedication for the benefit
of the public at large and to the detriment of our heirs and
successors. We intend this dedication to be an overt act of
relinquishment in perpetuity of all present and future rights to this
software under copyright law.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
IN NO EVENT SHALL THE AUTHORS BE LIABLE FOR ANY CLAIM, DAMAGES OR
OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE,
ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.

For more information, please refer to <http://unlicense.org/>
```

### WTFPL 2 (spark-md5, dual-licensed "WTFPL OR MIT"; MIT elected above)

```text
            DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
                    Version 2, December 2004

 Copyright (C) 2015 André Cruz <amdfcruz@gmail.com>

 Everyone is permitted to copy and distribute verbatim or modified
 copies of this license document, and changing it is allowed as long
 as the name is changed.

            DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
   TERMS AND CONDITIONS FOR COPYING, DISTRIBUTION AND MODIFICATION

  0. You just DO WHAT THE FUCK YOU WANT TO.
```

### Mozilla Public License 2.0 (MPL-2.0)

Components: `github.com/go-sql-driver/mysql` v1.10.0 (Copyright (c) the Go-MySQL-Driver Authors, https://github.com/go-sql-driver/mysql) and `@novnc/novnc` 1.7.0 (Copyright (C) 2022 The noVNC authors, https://github.com/novnc/noVNC; also lists MIT for `vendor/pako` and BSD-style for `core/des.js`), `dompurify` is available under MPL-2.0 but used under Apache-2.0.

The full license text is available at https://www.mozilla.org/en-US/MPL/2.0/ and is shipped with the packages (`LICENSE` in the go-sql-driver/mysql module, `docs/LICENSE.MPL-2.0` in the npm package). The MPL-covered source files are used unmodified; their Source Form is available from the URLs above and from the Go module proxy / npm registry at the versions listed. This license applies only to those files and does not extend to the rest of HomeHub.

Notice from noVNC LICENSE.txt:

```text
noVNC is Copyright (C) 2022 The noVNC authors
(./AUTHORS)

The noVNC core library files are licensed under the MPL 2.0 (Mozilla
Public License 2.0). The noVNC core library is composed of the
Javascript code necessary for full noVNC operation. This includes (but
is not limited to):

    core/**/*.js
    app/*.js
    test/playback.js

The HTML, CSS, font and images files that included with the noVNC
source distibution (or repository) are not considered part of the
noVNC core library and are licensed under more permissive licenses.
The intent is to allow easy integration of noVNC into existing web
sites and web applications.

The HTML, CSS, font and image files are licensed as follows:

    *.html                     : 2-Clause BSD license

    app/styles/*.css           : 2-Clause BSD license

    app/styles/Orbitron*       : SIL Open Font License 1.1
                                 (Copyright 2009 Matt McInerney)

    app/images/                : Creative Commons Attribution-ShareAlike
                                 http://creativecommons.org/licenses/by-sa/3.0/

Some portions of noVNC are copyright to their individual authors.
Please refer to the individual source files and/or to the noVNC commit
history: https://github.com/novnc/noVNC/commits/master

The are several files and projects that have been incorporated into
the noVNC core library. Here is a list of those files and the original
licenses (all MPL 2.0 compatible):

    core/base64.js          : MPL 2.0

    core/des.js             : Various BSD style licenses

    vendor/pako/            : MIT

Any other files not mentioned above are typically marked with
a copyright/license header at the top of the file. The default noVNC
license is MPL-2.0.

The following license texts are included:

    docs/LICENSE.MPL-2.0
    docs/LICENSE.OFL-1.1
    docs/LICENSE.BSD-3-Clause (New BSD)
    docs/LICENSE.BSD-2-Clause (Simplified BSD / FreeBSD)
    vendor/pako/LICENSE (MIT)

Or alternatively the license texts may be found here:

    http://www.mozilla.org/MPL/2.0/
    http://scripts.sil.org/OFL
    http://en.wikipedia.org/wiki/BSD_licenses
    https://opensource.org/licenses/MIT
```

### SIL Open Font License 1.1 (OFL-1.1)

Fonts: Inter Variable 4.001 (Copyright 2016 The Inter Project Authors, https://github.com/rsms/inter) and the KaTeX_* fonts (Copyright (c) 2009-2010 Design Science, Inc.; Copyright (c) 2014-2018 Khan Academy, with Reserved Font Name "KaTeX_*").

```text
SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007
-----------------------------------------------------------

PREAMBLE
The goals of the Open Font License (OFL) are to stimulate worldwide
development of collaborative font projects, to support the font creation
efforts of academic and linguistic communities, and to provide a free and
open framework in which fonts may be shared and improved in partnership
with others.

The OFL allows the licensed fonts to be used, studied, modified and
redistributed freely as long as they are not sold by themselves. The
fonts, including any derivative works, can be bundled, embedded, 
redistributed and/or sold with any software provided that any reserved
names are not used by derivative works. The fonts and derivatives,
however, cannot be released under any other type of license. The
requirement for fonts to remain under this license does not apply
to any document created using the fonts or their derivatives.

DEFINITIONS
"Font Software" refers to the set of files released by the Copyright
Holder(s) under this license and clearly marked as such. This may
include source files, build scripts and documentation.

"Reserved Font Name" refers to any names specified as such after the
copyright statement(s).

"Original Version" refers to the collection of Font Software components as
distributed by the Copyright Holder(s).

"Modified Version" refers to any derivative made by adding to, deleting,
or substituting -- in part or in whole -- any of the components of the
Original Version, by changing formats or by porting the Font Software to a
new environment.

"Author" refers to any designer, engineer, programmer, technical
writer or other person who contributed to the Font Software.

PERMISSION & CONDITIONS
Permission is hereby granted, free of charge, to any person obtaining
a copy of the Font Software, to use, study, copy, merge, embed, modify,
redistribute, and sell modified and unmodified copies of the Font
Software, subject to the following conditions:

1) Neither the Font Software nor any of its individual components,
in Original or Modified Versions, may be sold by itself.

2) Original or Modified Versions of the Font Software may be bundled,
redistributed and/or sold with any software, provided that each copy
contains the above copyright notice and this license. These can be
included either as stand-alone text files, human-readable headers or
in the appropriate machine-readable metadata fields within text or
binary files as long as those fields can be easily viewed by the user.

3) No Modified Version of the Font Software may use the Reserved Font
Name(s) unless explicit written permission is granted by the corresponding
Copyright Holder. This restriction only applies to the primary font name as
presented to the users.

4) The name(s) of the Copyright Holder(s) or the Author(s) of the Font
Software shall not be used to promote, endorse or advertise any
Modified Version, except to acknowledge the contribution(s) of the
Copyright Holder(s) and the Author(s) or with their explicit written
permission.

5) The Font Software, modified or unmodified, in part or in whole,
must be distributed entirely under this license, and must not be
distributed under any other license. The requirement for fonts to
remain under this license does not apply to any document created
using the Font Software.

TERMINATION
This license becomes null and void if any of the above conditions are
not met.

DISCLAIMER
THE FONT SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO ANY WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT
OF COPYRIGHT, PATENT, TRADEMARK, OR OTHER RIGHT. IN NO EVENT SHALL THE
COPYRIGHT HOLDER BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
INCLUDING ANY GENERAL, SPECIAL, INDIRECT, INCIDENTAL, OR CONSEQUENTIAL
DAMAGES, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF THE USE OR INABILITY TO USE THE FONT SOFTWARE OR FROM
OTHER DEALINGS IN THE FONT SOFTWARE.
```

