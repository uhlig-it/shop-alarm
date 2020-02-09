require 'json'

module MQTT
  module Blink1
    class Interpreter
      Error = Class.new(StandardError)
      class UnexpectedMessageFormat < Error; end

      def initialize(device)
        @blink1 = device
      end

      def interpret(msg)
        message = JSON.parse(msg)

        if message.keys.size != 1
          raise UnexpectedMessageFormat, "Expecting excactly one key, but got #{message.keys}"
        end

        command = message.keys.first

        if command == 'color'
          args = message['color']

          if args['fade']
            @blink1.fade_to_rgb(
              validate_numericality(args['fade'], 'color["fade"]'),
              validate_color(args['red'], 'color["red"]'),
              validate_color(args['green'], 'color["green"]'),
              validate_color(args['blue'], 'color["blue"]')
            )
          else
            @blink1.set_rgb(
              validate_color(args['red'], 'color["red"]'),
              validate_color(args['green'], 'color["green"]'),
              validate_color(args['blue'], 'color["blue"]')
            )
          end
        else
          raise Error, "Unexpected key #{message.key}"
        end
      end

      private

      def validate_color(value, key)
        color = validate_numericality(value, key)

        unless (0..255).include?(color)
          raise UnexpectedMessageFormat, "#{key} must be in 0..255, but it is #{value}"
        end

        color
      end

      def validate_numericality(value, key)
        unless value.is_a? Numeric
          raise UnexpectedMessageFormat, "#{key} must be numeric, but it is #{value}"
        end

        value.to_i
      end
    end
  end
end
