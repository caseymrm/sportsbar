APP=Sportsbar
IDENTIFIER=sportsbar.caseymrm.github.com
REPO=caseymrm/sportsbar

# Codesigning identity. Defaults to the Developer ID cert so release zips
# are notarizable; anyone without the cert can still build ad-hoc:
#   make IDENTITY=- zip
IDENTITY ?= Developer ID Application: Casey Muller (AZGE7WP274)

# menuet ships a shared bundling Makefile (menuet.mk) that handles plist
# generation, codesign, zip-for-release, etc. Find it via the Go module cache
# so we track the version pinned in go.mod.
MENUET_MK := $(shell go list -m -f '{{.Dir}}' github.com/caseymrm/menuet/v2)/menuet.mk

include $(MENUET_MK)

# Override the $(BINARY) rule from menuet.mk to produce a universal arm64+amd64
# binary instead of host-arch only. CGO_ENABLED=1 because menuet uses cgo
# (Cocoa / UserNotifications frameworks); macOS's clang ships both arch
# toolchains so cross-arch cgo build works without extra setup.
$(BINARY): $(SOURCES)
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -o $(BINARY).arm64
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -o $(BINARY).amd64
	lipo -create -output $(BINARY) $(BINARY).arm64 $(BINARY).amd64
	rm -f $(BINARY).arm64 $(BINARY).amd64

# notarize comes from menuet.mk (v2.10.4+): submits via the AC_NOTARY
# keychain profile, staples, and re-zips.
