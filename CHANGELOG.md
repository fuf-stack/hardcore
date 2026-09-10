# Changelog

## [2.0.0](https://github.com/fuf-stack/hardcore/compare/v1.2.0...v2.0.0) (2026-09-10)


### ⚠ BREAKING CHANGES

* require Go 1.27 and migrate health serialization to JSON v2

### Features

* **database:** add credential-safe connection URL parsing ([c9e6124](https://github.com/fuf-stack/hardcore/commit/c9e6124718a1685c92d16dc6986040500a96f553))


### Bug Fixes

* **deps:** update module github.com/go-sql-driver/mysql to v1.10.1 ([31bb300](https://github.com/fuf-stack/hardcore/commit/31bb3001c928f78f3e88856d65ef7eb0674dc50b))
* **deps:** update module github.com/go-sql-driver/mysql to v1.10.1 ([8f9873c](https://github.com/fuf-stack/hardcore/commit/8f9873c74674595865dd78a7802cbecfb981202b))


### Build System

* require Go 1.27 and migrate health serialization to JSON v2 ([cd31f99](https://github.com/fuf-stack/hardcore/commit/cd31f99b5a074d7763b4826b6b64f2db66d2ea85))

## [1.2.0](https://github.com/fuf-stack/hardcore/compare/v1.1.0...v1.2.0) (2026-09-10)


### Features

* **rpc:** add safe unary server error boundaries ([60b8a3e](https://github.com/fuf-stack/hardcore/commit/60b8a3e3d49e3fefec4a62521b805be6e4713b88))


### Bug Fixes

* **deps:** update module google.golang.org/protobuf to v1.36.12 ([00f0ede](https://github.com/fuf-stack/hardcore/commit/00f0ededc7ca952e7cfa532f9316e1e8cca1582c))
* **deps:** update module google.golang.org/protobuf to v1.36.12 ([21b1fc9](https://github.com/fuf-stack/hardcore/commit/21b1fc9f8d1c2523085a8a50196e37e87cfc8ee1))

## [1.1.0](https://github.com/fuf-stack/hardcore/compare/v1.0.1...v1.1.0) (2026-09-10)


### Features

* **database:** add SQL lifecycle and post-drain cleanup ([32858ab](https://github.com/fuf-stack/hardcore/commit/32858abca6020ca7a4e097813a65b6f44957fa53))


### Bug Fixes

* **ci:** resolve PostgreSQL port in step context ([247a168](https://github.com/fuf-stack/hardcore/commit/247a168315366157be03a1126b23d57d23ba3699))

## [1.0.1](https://github.com/fuf-stack/hardcore/compare/v1.0.0...v1.0.1) (2026-09-10)


### Bug Fixes

* **release:** use Go-compatible root module tags ([161a6cc](https://github.com/fuf-stack/hardcore/commit/161a6cc2cc622c8c3268eeddf82bbcf5927bd8f2))

## 1.0.0 (2026-09-10)


### Features

* establish service lifecycle and health foundations ([c8b5bea](https://github.com/fuf-stack/hardcore/commit/c8b5bea2b9493e0ff20e1ed5179715b7b3a6f6f7))
