SUDO ?= sudo
TAG ?= flobar/pcwusers
PORTS ?= 8080:80

default: docker-run

pcwusers: main.go
	CGO_ENABLED=0 go build .

.PHONY: docker-build
docker-build: Dockerfile pcwusers
	${SUDO} docker build -t ${TAG} .

.PHONY: docker-run
docker-run: docker-build
	${SUDO} docker run -p ${PORTS} -t ${TAG}

.PHONY: docker-push
docker-push: docker-build
	${SUDO} docker push ${TAG}

.PHONY: clean
clean:
	$(RM) pcwusers
