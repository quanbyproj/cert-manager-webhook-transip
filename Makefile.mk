#
#   Copyright 2024 Quanby Services B.V.
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
#

# Configuration Variables
REGISTRY_HOST = docker.io
USERNAME = quanby
NAME = $(shell basename $(PWD))
RELEASE_SUPPORT_PATH = $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))/.make-release-support
IMAGE = $(REGISTRY_HOST)/$(USERNAME)/$(NAME)

# Retrieve Version and Tag from Release Support
VERSION = $(shell . $(RELEASE_SUPPORT_PATH) ; getVersion)
TAG = $(shell . $(RELEASE_SUPPORT_PATH) ; getTag)

# Set Shell
SHELL = /bin/bash

# Phony Targets Declaration
.PHONY: pre-build docker-build post-build build release patch-release minor-release major-release tag check-status \
        check-release showver push do-push post-push

# Main Build Target
build: pre-build docker-build post-build

# Pre-Build Hook
pre-build:
	@echo "Running pre-build steps..."

# Post-Build Hook
post-build:
	@echo "Running post-build steps..."

# Post-Push Hook
post-push:
	@echo "Running post-push steps..."

# Docker Build Target
docker-build: .release
	@echo "Building Docker image..."
	docker build -t $(IMAGE):$(VERSION) . || { echo "ERROR: Docker build failed"; exit 1; }

	# Check Docker version and apply appropriate tagging
	@DOCKER_MAJOR=$(shell docker -v | sed -e 's/.*version //' -e 's/,.*//' | cut -d\. -f1) ; \
	DOCKER_MINOR=$(shell docker -v | sed -e 's/.*version //' -e 's/,.*//' | cut -d\. -f2) ; \
	if [ $$DOCKER_MAJOR -eq 1 ] && [ $$DOCKER_MINOR -lt 10 ] ; then \
		echo "Tagging image with -f flag for older Docker versions"; \
		docker tag -f $(IMAGE):$(VERSION) $(IMAGE):latest || { echo "ERROR: Docker tagging failed"; exit 1; } ; \
	else \
		echo "Tagging image without -f flag for newer Docker versions"; \
		docker tag $(IMAGE):$(VERSION) $(IMAGE):latest || { echo "ERROR: Docker tagging failed"; exit 1; } ; \
	fi

# Release File Creation
.release:
	@echo "Creating .release file..."
	@if [ ! -f .release ]; then \
		echo "release=0.0.0" > .release; \
		echo "tag=$(NAME)-0.0.0" >> .release; \
		echo "INFO: .release file created"; \
	fi
	@cat .release

# Release Target
release: check-status check-release build push

# Push Docker Images
push: do-push post-push

do-push:
	@echo "Pushing Docker images..."
	docker push $(IMAGE):$(VERSION) || { echo "ERROR: Docker push for version failed"; exit 1; }
	docker push $(IMAGE):latest || { echo "ERROR: Docker push for latest tag failed"; exit 1; }

# Snapshot Build and Push
snapshot: build push

# Show Version
showver: .release
	@. $(RELEASE_SUPPORT_PATH); getVersion

# Tagging for Patch, Minor, and Major Releases
tag-patch-release: VERSION := $(shell . $(RELEASE_SUPPORT_PATH); nextPatchLevel)
tag-patch-release: .release tag

tag-minor-release: VERSION := $(shell . $(RELEASE_SUPPORT_PATH); nextMinorLevel)
tag-minor-release: .release tag

tag-major-release: VERSION := $(shell . $(RELEASE_SUPPORT_PATH); nextMajorLevel)
tag-major-release: .release tag

# Release Targets
patch-release: tag-patch-release release
	@echo $(VERSION)

minor-release: tag-minor-release release
	@echo $(VERSION)

major-release: tag-major-release release
	@echo $(VERSION)

# Tagging Logic
tag: TAG=$(shell . $(RELEASE_SUPPORT_PATH); getTag $(VERSION))
tag: check-status
	@. $(RELEASE_SUPPORT_PATH); \
	! tagExists $(TAG) || (echo "ERROR: Tag $(TAG) for version $(VERSION) already exists in git" >&2 && exit 1);
	@. $(RELEASE_SUPPORT_PATH); setRelease $(VERSION)
	@git add .release
	@git commit -m "Bumped to version $(VERSION)" || { echo "ERROR: Git commit failed"; exit 1; }
	@git tag $(TAG) || { echo "ERROR: Git tagging failed"; exit 1; }
	@[ -n "$(shell git remote -v)" ] && git push --tags || { echo "ERROR: Git push failed"; exit 1; }

# Check for Outstanding Changes
check-status:
	@. $(RELEASE_SUPPORT_PATH); \
	! hasChanges || (echo "ERROR: There are still outstanding changes" >&2 && exit 1)

# Check Release Status
check-release: .release
	@. $(RELEASE_SUPPORT_PATH); \
	tagExists $(TAG) || (echo "ERROR: Version not yet tagged in git. Use [minor, major, patch]-release." >&2 && exit 1);
	@. $(RELEASE_SUPPORT_PATH); \
	! differsFromRelease $(TAG) || (echo "ERROR: Current directory differs from tagged $(TAG). Use [minor, major, patch]-release." && exit 1)
