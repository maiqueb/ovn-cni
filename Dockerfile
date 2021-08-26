FROM golang:1.16 as builder

ADD . /usr/src/k8s-ovn-cni

ENV HTTP_PROXY $http_proxy
ENV HTTPS_PROXY $https_proxy

WORKDIR /usr/src/k8s-ovn-cni
RUN mkdir -p /usr/src/k8s-ovn-cni/build/ && \
    GOOS=linux CGO_ENABLED=0 go build -o \
     /usr/src/k8s-ovn-cni/build/k8s-ovn-controller \
     github.com/maiqueb/ovn-cni/cmd/controller && \
    GOOS=linux CGO_ENABLED=0 go build -o \
     /usr/src/k8s-ovn-cni/build/ovn-cni \
     github.com/maiqueb/ovn-cni/cmd/cni

FROM registry.access.redhat.com/ubi8/ubi-minimal
COPY --from=builder /usr/src/k8s-ovn-cni/build/k8s-ovn-controller /usr/bin/
COPY --from=builder /usr/src/k8s-ovn-cni/build/ovn-cni /usr/bin/
WORKDIR /

LABEL io.k8s.display-name="OVN-CNI-CONTROLLER"

ENTRYPOINT ["/usr/bin/k8s-ovn-controller", "--alsologtostderr"]
