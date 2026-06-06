APP=Sportsbar
IDENTIFIER=sportsbar.caseymrm.github.com
REPO=caseymrm/sportsbar

# menuet ships a shared bundling Makefile (menuet.mk) that handles plist
# generation, codesign, zip-for-release, etc. Find it via the Go module cache
# so we track the version pinned in go.mod.
MENUET_MK := $(shell go list -m -f '{{.Dir}}' github.com/caseymrm/menuet)/menuet.mk

include $(MENUET_MK)
