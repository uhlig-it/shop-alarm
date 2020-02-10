require 'json'
require_relative 'commands'

module MQTT
  module Blink1
    Error = Class.new(StandardError)
    class UnexpectedMessageFormat < Error; end
    class UnrecognizedCommand < Error; end

    class Interpreter
      def initialize(device)
        @blink1 = device
      end

      def interpret(msg)
        message = JSON.parse(msg)

        if message.is_a?(Hash)
          if message.keys.size != 1
            raise UnexpectedMessageFormat, "Expecting excactly one key, but got #{message.keys}"
          end

          key = message.keys.first
          produceCommand(key).call(message[key])
        else
          produceCommand(message).call
        end
      end

      private

      def produceCommand(key)
        raise UnrecognizedCommand, key unless Commands.const_defined?(key.capitalize, false)
        Commands.const_get(key.capitalize).new(@blink1)
      end
    end
  end
end
