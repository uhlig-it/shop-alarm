# MQTT Keyboard

Listens to an input device via `evdev` and, on Enter, publishes the entered text to an MQTT topic. A listener then, on message, disables the alarm system. Optionally, a blink1 device provides some visual feedback.

# Synopsis

Example:

```command
$ mqtt-keyboard --device DEVICE --timeout TIMEOUT --mqtt MQTT_URL --topic TOPIC
```

The program will start to listen for events on `DEVICE`. When `ENTER` is pressed, any input that was typed within `TIMEOUT` will be published to the `TOPIC` at [`MQTT_URL`](https://github.com/mqtt/mqtt.github.io/wiki/URI-Scheme).

Example:

```command
$ mqtt-keyboard --device /dev/input/event0 --timeout 3 --mqtt-host mqtts://user:password@example.com --topic oldpi/keyboard
```

Anything typed on `/dev/input/event0` will be published to `oldpi/keyboard`. If there are more than 3 seconds between two consecutive keystrokes, all previous input will be ignored.

Listening to these events is as simple as:

```command
$ mosquitto_sub \
  --cafile /usr/local/etc/openssl/cert.pem \
  --url mqtts://user:password@example.com/oldpi/keyboard \
  -F '\e[92m %I %t: \e[96m%p\e[0m'
```

# Deployment

```bash
$ cd deployment
$ ansible-playbook playbook.yml
```

Ansible will deploy the service, enable and start it.

# TODO

* Use MAC address as client ID (there shall be only one keyboard; the last one connecting wins)
* Tests?
* Can we [blink the LED](https://hewner.github.io/2006/08/21/evdev-for-ruby-with-morse-code/)?
* Add an SSD1306 display
