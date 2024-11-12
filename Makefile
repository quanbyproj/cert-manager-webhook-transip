include Makefile.mk

stable: check-status patch-release
	docker tag $(IMAGE):$(VERSION) $(IMAGE):stable
	docker push $(IMAGE):stable


