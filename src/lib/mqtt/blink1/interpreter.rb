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

      def interpret(payload)
        case message = JSON.parse(payload)
        when Hash
          if message.keys.size != 1
            raise UnexpectedMessageFormat, "Expecting excactly one key, but got #{message.keys}"
          end

          key = message.keys.first
          produceCommand(key).call(message[key])
        when Array
          message.each do |msg|
            interpret(msg.to_json)
          end
        when String
          produceCommand(message).call
        else
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
