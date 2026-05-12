.PHONY: all build test vet fmt vuln verify clean

GO ?= /home/innovedia-admin/.local/go/bin/go

all: verify

build:
	$(GO) build ./...

test:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

vuln:
	@command -v govulncheck >/dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

# Run the full quality gate. Mirrors what Printing Press runs on every
# generated CLI; we hold ourselves to the same bar.
verify: vet test
	$(GO) build -o /tmp/echo-cli ./examples/echo-cli
	@/tmp/echo-cli greet ci --agent --select results.greeting | grep -q '"greeting":"hello, ci"' \
	  && echo "✓ echo-cli greet --select" \
	  || { echo "✗ echo-cli greet --select"; exit 1; }
	@/tmp/echo-cli list --csv --select results.name | grep -q '^name$$' \
	  && echo "✓ echo-cli list --csv unwraps envelope" \
	  || { echo "✗ echo-cli list --csv"; exit 1; }
	@/tmp/echo-cli fail --code 4 >/dev/null 2>&1; \
	  if [ "$$?" -eq 4 ]; then echo "✓ typed exit code 4 (auth)"; else echo "✗ exit code"; exit 1; fi
	@rm -f /tmp/echo-cli

clean:
	rm -f echo-cli /tmp/echo-cli /tmp/echo-out.json
