FROM alpine:latest
WORKDIR /root
COPY ./MqttClientTestTool_v1.5.0_Linux64 ./mqttclienttesttool
RUN chmod +x ./mqttclienttesttool
ENTRYPOINT ["./mqttclienttesttool"]
