require_relative 'router'

module MQTT
  class Blink1
    def initialize(broker:, topic:, device:, logger:)
      @router = MQTT::Router.new(broker: broker, logger: logger)
      @router.add_route('werkstatt/lock') do |_, message|
        logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
        case message
        when 'locked'
          device.set_rgb(128, 0, 0)
        when 'unlocked'
          device.set_rgb(0, 128, 0)
        when 'lock-failed'
          device.blink(0, 128, 0, 3)
        when 'unlock-failed'
          device.blink(128, 0, 0, 3)
        end
      end
    end

    def start!
      @router.start!
    end
  end
end
