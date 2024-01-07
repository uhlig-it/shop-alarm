# Shop Alarm

A workshop security system based on events published to MQTT.

# Components

## `MQTT::NFC` sensor

Publishes a `scanned` event to `werkstatt/nfc/status` as it is called via SSH. It passes the NFC tag's ID as well as the calling user agent (e.g. `iPhoneSteffen`).

## `MQTT::Telegram` actor

Subscribes to `werkstatt/telegram`. Upon `message` events, sends the payload of the event to Telegram.

## `MQQT::Lock` service

TODO

# FAQ

Q: What if we want to combine two events, or have a dependency? E.g. if the NFC event fires, but the lock is disarmed, the alarm event should not be published?

A: We need to find a way to query the status of a sensor. Perhaps we need a (compound. virtual) processor (the "Alarm System") that, upon one or more events, evaluates the state of multiple sensors in order to take some action (e.g. send a Telegram message or emit an `alarm` event).

# Deployment

```bash
$ cd deployment
$ ansible-playbook playbook.yml
```

Ansible will deploy the service, enable and start it.

# TODO

* Metrics, e.g. the lock state and when motion detected some, well, motion
  => some are useful as numeric data (e.g. how long an alert went on)
  => some are useful as events that can be show in graphs (e.g. motion detected, alarm disarmed, etc.) => https://gist.github.com/suhlig/049b068185f216824d33756dae99172a
* On first start, `motion` needs to be restarted; otherwise the camera will be almost black
* Some topics are still hardcoded (`werkstatt/lock`)
* Delay arming a couple of seconds after it being enabled, so we don't raise an alert on our way out
* Tests?
* Add an SSD1306 display, just for kicks
