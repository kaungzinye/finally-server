# Finally Server

Finally Server is the authenticated, self-hosted task authority for Finally clients. It is a public fork of Vikunja and retains Vikunja's upstream history, attribution, and AGPL-3.0-or-later license.

## Local backend setup

Install Go 1.26.4 and Mage 1.17.2, then build and run the server with a local SQLite database:

```bash
export VIKUNJA_DATABASE_TYPE=sqlite
export VIKUNJA_DATABASE_PATH="$PWD/finally.db"
export VIKUNJA_SERVICE_SECRET="replace-this-local-development-secret"

mage build
./vikunja user create --username finally --email finally@example.com --password 'local-password'
./vikunja
```

The server listens on `http://localhost:3456` by default. Use a strong, deployment-specific service secret outside local development.

## Versioned task API

Finally iOS and automation clients use only the narrow `/api/v2/finally` surface. Sign in with `POST /api/v2/finally/login`, then send the returned JWT as `Authorization: Bearer <token>`. API tokens are also accepted according to their configured scopes.

The minimal task lifecycle uses these authenticated routes:

- `POST /api/v2/finally/projects/{project}/tasks` creates a task.
- `GET /api/v2/finally/tasks/{projecttask}` reads a task.
- `PUT /api/v2/finally/tasks/{projecttask}` updates a task.
- `POST /api/v2/finally/tasks/{projecttask}/complete` completes a task.
- `DELETE /api/v2/finally/tasks/{projecttask}` deletes a task.

The matching machine-readable contract is served at `/api/v2/finally/openapi.json` and contains login, task lifecycle, and planning-context operations. The inherited web application and server administration continue to use the full `/api/v2` surface, documented at `/api/v2/openapi.json` with its local Scalar reference at `/api/v2/docs`. New client API work belongs under `/api/v2/finally`; inherited `/api/v1` routes remain frozen.

## Private calendar context

Configure a Google OAuth client and Redis before connecting calendar accounts:

```bash
export VIKUNJA_KEYVALUE_TYPE=redis
export VIKUNJA_REDIS_ENABLED=true
export VIKUNJA_CALENDAR_ENCRYPTIONKEY="stable-random-calendar-key"
export VIKUNJA_CALENDAR_GOOGLE_CLIENTID="google-client-id"
export VIKUNJA_CALENDAR_GOOGLE_CLIENTSECRET="google-client-secret"
```

`POST /api/v2/finally/calendar/accounts` exchanges a Google authorization code, `GET /api/v2/finally/calendar/accounts` lists the current user's connections, and `DELETE /api/v2/finally/calendar/accounts/{account}` revokes one. `POST /api/v2/finally/calendar/context` fetches event descriptions and attendee context for a bounded planning window.

Finally Server encrypts Google tokens with the stable calendar encryption key before writing them to Redis. Calendar event bodies remain request-scoped: the server returns them to the authenticated planning client without writing them to task storage, Redis, routine logs, or database backups. Calendar connection setup returns a recoverable service-unavailable response when durable Redis storage or stable encryption key material is not configured.

---

<img src="https://vikunja.io/images/vikunja-logo.svg" alt="" style="display: block;width: 50%;margin: 0 auto;" width="50%"/>

[![Build Status](https://github.com/go-vikunja/vikunja/actions/workflows/ci.yml/badge.svg)](https://github.com/go-vikunja/vikunja/actions/workflows/ci.yml)
[![License: AGPL-3.0-or-later](https://img.shields.io/badge/License-AGPL--3.0--or--later-blue.svg)](LICENSE)
[![Install](https://img.shields.io/badge/download-v2.3.0-brightgreen.svg)](https://vikunja.io/docs/installing)
[![Docker Pulls](https://img.shields.io/docker/pulls/vikunja/vikunja.svg)](https://hub.docker.com/r/vikunja/vikunja/)
[![Swagger Docs](https://img.shields.io/badge/swagger-docs-brightgreen.svg)](https://try.vikunja.io/api/v1/docs)
[![Go Report Card](https://goreportcard.com/badge/code.vikunja.io/api)](https://goreportcard.com/report/code.vikunja.io/api)

# Vikunja

> The Todo-app to organize your life.

If Vikunja is useful to you, please consider [buying me a coffee](https://www.buymeacoffee.com/kolaente), [sponsoring me on GitHub](https://github.com/sponsors/kolaente) or buying [a sticker pack](https://vikunja.io/stickers).
I'm also offering [a hosted version of Vikunja](https://vikunja.cloud/) if you want a hassle-free solution for yourself or your team.

## Table of contents

- [Security Reports](#security-reports)
- [Features](#features)
- [Docs](#docs)
	- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)
	- [Unsplash Images](#unsplash-images)

## Security Reports

If you find any security-related issues you don't want to disclose publicly, please use [the contact information on our website](https://vikunja.io/contact/#security).

## Features

See [the features page](https://vikunja.io/features/) on our website for a more exhaustive list or 
try it on [try.vikunja.io](https://try.vikunja.io)!

## Docs

* [Installing](https://vikunja.io/docs/installing/)
* [Build from source](https://vikunja.io/docs/build-from-sources/)
* [Development setup](https://vikunja.io/docs/development/)
* [Magefile](https://vikunja.io/docs/magefile/)
* [Testing](https://vikunja.io/docs/testing/)

All docs can be found on [the Vikunja home page](https://vikunja.io/docs/).

### Roadmap

See [the roadmap](https://my.vikunja.cloud/share/QFyzYEmEYfSyQfTOmIRSwLUpkFjboaBqQCnaPmWd/auth) (hosted on Vikunja!) for more!

## Contributing

Please check out the contribution guidelines on [the website](https://vikunja.io/docs/development/).

## License

Most of this repository is licensed under [AGPL‑3.0‑or‑later](LICENSE).
The contents of [`desktop/`](desktop/) are licensed under
[GPL‑3.0‑or‑later](desktop/LICENSE).

### Unsplash Images

Background images from Unsplash are distributed under the [Unsplash License](https://unsplash.com/license). The license requires giving credit to the photographer and Unsplash. See [Unsplash’s terms](https://unsplash.com/terms) for more information.
