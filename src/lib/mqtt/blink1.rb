require_relative 'router'

module MQTT
  class Blink1
    #
    # topic: where we publish the calculated Blink1 pattern
    #
    def initialize(broker:, topic:, device:, logger:)
      @router = MQTT::Router.new(broker: broker, logger: logger)

      @router.add_route('werkstatt/lock') do |b, t, message|
        logger.debug(self.class.name) { "Received in #{t}: #{message}" }

        case message
        when 'armed'
          logger.debug(self.class.name) { "Setting LED to red" }
          device.set_rgb(255, 0, 0)
          b.publish(topic, '{"color":{"r":255,"g":0,"b":0}}')
        when 'disarmed'
          logger.debug(self.class.name) { "Setting LED to green" }
          device.set_rgb(0, 255, 0)
          b.publish(topic, '{"color":{"r":0,"g":255,"b":0}}')
        when 'arm-failed'
          logger.debug(self.class.name) { "Blinking green" }
          device.blink(0, 255, 0, 3)
          b.publish(topic, '{"blink":{"count":"3","color":{"r":0,"g":255,"b":0}}}')
        when 'disarm-failed'
          logger.debug(self.class.name) { "Blinking red" }
          device.blink(255, 0, 0, 3)
          b.publish(topic, '{"blink":{"count":"3","color":{"r":255,"g":0,"b":0}}}')
        end
      end
    end

    def start!
      @router.start!
    end
  end
end
