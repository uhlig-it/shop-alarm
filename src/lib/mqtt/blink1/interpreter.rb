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
          Commands.const_get(key.capitalize).new(@blink1).call(message[key])
        else
          Commands.const_get(message.capitalize).new(@blink1).call
        end
      end
    end
  end
end
