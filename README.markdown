# MQTT Keyboard

Listens to an input device via `evdev` and, on Enter, publishes the entered text to an MQTT topic. A listener would than, on message, disable the alarm system.

# Installation

```command
$ apt install --yes ruby ruby-dev libevdev-dev
$ sudo gem install evdev mqtt
```

# Synopsis

Example:

```command
$ mqtt-keyboard --device DEVICE --topic TOPIC --timeout TIMEOUT --mqtt MQTT_URL
```

The program will start to listen for events on `DEVICE`. When `ENTER` is pressed, any input that was typed within `TIMEOUT` will be published to the `TOPIC` at [`MQTT_URL`](https://github.com/mqtt/mqtt.github.io/wiki/URI-Scheme).

Example:

```command
$ mqtt-keyboard --device /dev/input/event0 --timeout 3 --mqtt-host mqtts://user:password@example.com --topic oldpi/keyboard
```

Anything typed on `/dev/input/event0` will be published to `oldpi/keyboard`. If there are more than 3 seconds between two consecutive keystrokes, all previous input will be ignored.

# TODO

* Use MAC address as client ID (there shall be only one keyboard; the last one connecting wins)
* Tests?
* Can we [blink the LED](https://hewner.github.io/2006/08/21/evdev-for-ruby-with-morse-code/)?
