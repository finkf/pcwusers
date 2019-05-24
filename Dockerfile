FROM golang:alpine
MAINTAINER Florian Fink <finkf@cis.lmu.de>
ENV DATE='Fri 24 May 2019 05:21:11 PM CEST'

# ENV PCWAUTH_GIT=github.com/finkf/pcwauth
# ENV GO111MODULE=on
# RUN apk add git &&\
# 	go get -u ${PCWAUTH_GIT} &&\
# 	apk del git
COPY pcwusers /go/bin/
CMD pcwusers \
	-dsn "${MYSQL_USER}:${MYSQL_PASSWORD}@(db)/${MYSQL_DATABASE}" \
	-listen ':80' \
	-root-name ${PCW_ROOT_NAME} \
	-root-password ${PCW_ROOT_PASSWORD} \
	-root-email ${PCW_ROOT_EMAIL} \
	-root-institute ${PCW_ROOT_INSTITUTE} \
	-debug
