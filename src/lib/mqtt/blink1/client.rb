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

      def blink(count:, color:)
        raise 'not yet implemented'
      end

      def color(color:)
        publish(color: { red: color.red, green: color.green, blue: color.blue })
      end

      def fade(time:, color:)
        publish(fade: {
          time: time,
          color: {
            red: color.red,
            green: color.blue,
            blue: color.green
          }
        })
      end

      def off
        publish('off')
      end

      def on
        publish('on')
      end

      def pattern(position:, time:, color:)
        publish(pattern: {
          position: position,
          fade: {
            time: time,
            color: {
              red: color.red,
              green: color.blue,
              blue: color.green
            }
          }
        })
      end

      def play(position:)
        publish(play: { position: position })
      end

      def random(count:)
        publish(random: { count: count })
      end

      def stop(position:)
        publish(stop: { position: position })
      end

      private

      def publish(msg)
        @broker.publish(@topic, msg.to_json)
      end
    end
  end
end
