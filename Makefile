APP=Sportsbar
IDENTIFIER=sportsbar.caseymrm.github.com
REPO=caseymrm/sportsbar

# Codesigning identity. Defaults to ad-hoc ("-") so local builds and the
# release zip "just work" without an Apple Developer account. Override for
# real releases:
#   make IDENTITY="Developer ID Application: Casey Muller (TEAMID)" zip
IDENTITY ?= -

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

# Notarize the signed zip and staple the result. Requires:
#   - Developer ID Application certificate in keychain (real IDENTITY, not "-")
#   - One-time setup: xcrun notarytool store-credentials NotaryProfile \
#                     --apple-id you@example.com --team-id TEAMID --password APP_SPEC_PW
.PHONY: notarize
notarize: $(ZIPFILE)
	xcrun notarytool submit $(ZIPFILE) --keychain-profile NotaryProfile --wait
	xcrun stapler staple $(ESCAPED_APP).app
	rm -f $(ZIPFILE)
	zip -r $(ZIPFILE) $(ESCAPED_APP).app
