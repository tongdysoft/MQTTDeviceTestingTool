FROM alpine:latest
WORKDIR /root
COPY ./MQTTDeviceTestingTool ./MQTTDeviceTestingTool
RUN chmod +x ./MQTTDeviceTestingTool
ENTRYPOINT ["./MQTTDeviceTestingTool"]
