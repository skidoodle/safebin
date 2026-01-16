# Changelog

## [3.0.0](https://github.com/skidoodle/safebin/compare/v2.0.0...v3.0.0) (2026-01-16)


### ⚠ BREAKING CHANGES

* Docker volume paths and environment variables have been updated. The internal storage path in the container has changed from `/home/appuser/storage` to `/app/storage`. Existing deployments must update their volume mappings and environment variable names to maintain persistence.

### Code Refactoring

* relocate core logic to internal package and modernize project structure ([43be383](https://github.com/skidoodle/safebin/commit/43be383fdbfb0263036284b8beb0ce3c646db87c))

## [2.0.0](https://github.com/skidoodle/safebin/compare/v1.1.0...v2.0.0) (2026-01-16)


### ⚠ BREAKING CHANGES

* The encryption scheme and URL structure have been completely redesigned. Links generated with previous versions of safebin are no longer compatible and cannot be decrypted by this version.

### Features

* overhaul encryption to zero-knowledge at rest and modernize UI ([599347e](https://github.com/skidoodle/safebin/commit/599347e867444288fa58f8e358269121c5d32e36))

## [1.1.0](https://github.com/skidoodle/safebin/compare/v1.0.1...v1.1.0) (2026-01-14)


### Features

* implement chunked uploads and environment-based configuration ([1ccc80a](https://github.com/skidoodle/safebin/commit/1ccc80ad4e5b949a8f1d1f3a8b3b4e8c4d2e1353))

## [1.0.1](https://github.com/skidoodle/safebin/compare/v1.0.0...v1.0.1) (2026-01-14)


### Bug Fixes

* better dockerfile ([c1ecbe5](https://github.com/skidoodle/safebin/commit/c1ecbe567a24eb4e755f19fee68422025f3b15b2))

## 1.0.0 (2026-01-13)


### Features

* add automated release and docker workflow ([e40e6d0](https://github.com/skidoodle/safebin/commit/e40e6d01afd0067bba5d0cf4a9b1ff3d7122259f))
