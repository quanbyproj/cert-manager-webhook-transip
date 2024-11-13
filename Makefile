include Makefile.mk

ifndef IMAGE
$(error IMAGE is not set)
endif

ifndef VERSION
$(error VERSION is not set)
endif

.PHONY: stable

stable: check-status patch-release
	@echo "Tagging $(IMAGE):$(VERSION) as $(IMAGE):stable"
	docker tag $(IMAGE):$(VERSION) $(IMAGE):stable
	@echo "Pushing $(IMAGE):stable to the registry"
	docker push $(IMAGE):stable
