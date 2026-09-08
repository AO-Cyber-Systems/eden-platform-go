# eden-platform-go — local dev recipes.
#
# This repo also sits inside the eden-libs workspace, whose root justfile
# defines cross-package recipes (setup/generate/test/lint/dev-go/...). This
# file holds recipes scoped to eden-platform-go itself, following the same
# `dev-*` naming style.

# dev-quickstart mints the dev session through the production
# auth.Service.Login path and prints a curl proving the token + base URL
# work against an authenticated procedure. Binds to 127.0.0.1:8091 ONLY —
# never 8080 (permanently occupied elsewhere; see guardServerAddr in
# cmd/eden-platform-dev/devsession.go, which refuses to start on anything
# but a loopback address).

# seeds a demo tenant + mints a real dev session; prints a working curl. DEV-ONLY, IN-MEMORY.
dev-quickstart:
    SERVER_ADDR=127.0.0.1:8091 go run -tags dev ./cmd/eden-platform-dev
