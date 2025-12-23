.PHONY: build-url-shortener-cloud
build-url-shortener-cloud:
	docker build -t url-shortener/url-shortener:cloud \
		-f Dockerfile.cloud \
		.

.PHONY: run-url-shortener-cloud
run-url-shortener-cloud:
	docker run -e PORT=4444 -p 3000:4444 \
		--name url-shortener \
		--rm \
		url-shortener/url-shortener:cloud
