require 'json'

module MQTT
  module Blink1
    class Color
      attr_reader :red, :green, :blue

      def initialize(red:, green:, blue:)
        @red, @green, @blue = red, green, blue
      end
    end

    class Client
      def initialize(broker:, topic:, logger:)
        @broker = broker
        @topic = topic
        @logger = logger
      end

      def blink(count:, red:, green:, blue:)
        publish(blink: {
          count: count,
          color: {
            red: red,
            green: green,
            blue: blue
          }
        })
      end

      def color(red:, green:, blue:)
        publish(color: { red: red, green: green, blue: blue })
      end

      def fade(time:, red:, green:, blue:)
        publish(fade: {
          time: time,
          color: {
            red: red,
            green: green,
            blue: blue
          }
        })
      end

      def off
        publish('off')
      end

      def on
        publish('on')
      end

      def pattern(position:, time:, red:, green:, blue:)
        publish(pattern: {
          position: position,
          fade: {
            time: time,
            color: {
              red: red,
              green: green,
              blue: blue
            }
          }
        })
      end

      def play(position)
        publish(play: { position: position })
      end

      def random(count=1)
        publish(random: { count: count })
      end

      def stop(position)
        publish(stop: { position: position })
      end

      private

      def publish(msg)
        @broker.publish(@topic, msg.to_json)
      end
    end
  end
end
