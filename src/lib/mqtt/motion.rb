require_relative 'router'

module MQTT
  module Motion
    def initialize(broker:, topic:, motion:, logger:)
      @router = MQTT::Router.new(broker: broker, logger: logger)

      @router.add_route(topic) do |_, message|
        logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
        motion.send(message)
      rescue MQTT::Motion::Error => e
        logger.error(self.class.name) { e.message }
      end
    end

    def start!
      @router.start!
    end
  end
end
