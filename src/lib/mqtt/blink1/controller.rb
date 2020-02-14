require 'blink1'
require_relative 'interpreter'

module MQTT
  module Blink1
    # TODO This could be a route
    class Controller
      def initialize(broker:, topic:, logger:)
        @broker = broker
        @topic = topic
        @logger = logger

        @blink1 = ::Blink1.new
        @interpreter = Interpreter.new(@blink1)
      end

      def start!
        @blink1.open

        @broker.get(@topic) do |topic, message|
          @logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
          @interpreter.interpret(message)
        rescue MQTT::Blink1::Error => e
          @logger.error(self.class.name) { e.message }
        end

        @blink1.close
      end
    end
  end
end
