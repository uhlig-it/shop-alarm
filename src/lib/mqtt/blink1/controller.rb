require 'blink1'
require_relative 'message_parser'

module MQTT
  module Blink1
    class Controller
      def initialize(mqtt:, topic:, logger:)
        @mqtt = mqtt
        @topic = topic
        @logger = logger

        @blink1 = ::Blink1.new
        @interpreter = Interpreter.new(@blink1)
      end

      def start!
        @blink1.open

        @mqtt.get(@topic) do |topic, message|
          @logger.debug(self.class.name) { "Received in #{topic}: #{message}" }
          @interpreter.interpret(message))
        rescue Interpreter::Error => e
          @logger.error(self.class.name) { e.message }
        end

        @blink1.close
      end
    end
  end
end
