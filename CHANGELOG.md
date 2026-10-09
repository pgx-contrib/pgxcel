# Changelog

## [1.0.0](https://github.com/pgx-contrib/pgxcel/compare/v0.0.1...v1.0.0) (2026-10-09)


### ⚠ BREAKING CHANGES

* pgxcel.Transpile is now pgxcel.Where.
* Transpile now takes a *cel.Ast from cel.dev/cel-go rather than github.com/google/cel-go. The two are distinct types with no conversion between them, so callers must migrate their own cel-go import in the same change.

### Features

* Add CEL to Postgres WHERE transpiler ([4129238](https://github.com/pgx-contrib/pgxcel/commit/41292389b9afc17a2ceb52b360015f7b6be50d23))
* move to the cel.dev/cel-go module path ([#37](https://github.com/pgx-contrib/pgxcel/issues/37)) ([5a09b06](https://github.com/pgx-contrib/pgxcel/commit/5a09b06382462510bdc6c0f5cde24f658d7f554f))
* support CEL `in` operator as SQL IN clause ([32e8f3d](https://github.com/pgx-contrib/pgxcel/commit/32e8f3d803b56136c7495d29088c76f1394ac024))
* support CEL string membership functions and add WithFunctions aliasing ([22521d8](https://github.com/pgx-contrib/pgxcel/commit/22521d8e7f09410aa4937fe7004782645fc6e044))


### Bug Fixes

* correct SQL semantics of Where and add Postgres integration tests ([#46](https://github.com/pgx-contrib/pgxcel/issues/46)) ([198f66c](https://github.com/pgx-contrib/pgxcel/commit/198f66c995eb09d2a3ae7b684a867b31f6de7681))
* **github:** correct action versions in update.yml ([a3efe35](https://github.com/pgx-contrib/pgxcel/commit/a3efe357b688c0223771909d6f60c55818aaf84b))


### Code Refactoring

* rename Transpile to Where ([287fc24](https://github.com/pgx-contrib/pgxcel/commit/287fc2401627af9c3f1e8643d9a18e7cd06ec886))
