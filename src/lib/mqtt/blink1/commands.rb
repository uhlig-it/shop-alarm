module MQTT
  module Blink1
    module Commands
      class Base
        attr_reader :blink1

        def initialize(blink1)
          @blink1 = blink1
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

      class Color < Base
        def call(args)
          blink1.set_rgb(
            validate_color(args['red'], 'color["red"]'),
            validate_color(args['green'], 'color["green"]'),
            validate_color(args['blue'], 'color["blue"]')
          )
        end
      end

      class Fade < Base
        def call(args)
          blink1.fade_to_rgb(
            validate_numericality(args['time'], 'fade["time"]'),
            validate_color(args['color']['red'], 'color["red"]'),
            validate_color(args['color']['green'], 'color["green"]'),
            validate_color(args['color']['blue'], 'color["blue"]')
          )
        end
      end

      class Blink < Base
        def call(args)
          blink1.blink(
            validate_color(args['color']['red'], 'color["red"]'),
            validate_color(args['color']['green'], 'color["green"]'),
            validate_color(args['color']['blue'], 'color["blue"]'),
            validate_numericality(args['count'], 'blink["time"]')
          )
        end
      end

      class On < Base
        def call
          blink1.on
        end
      end

      class Off < Base
        def call
          blink1.off
        end
      end
    end
  end
end
