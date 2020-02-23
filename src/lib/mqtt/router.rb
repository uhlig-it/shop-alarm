require 'mqtt'

module MQTT
  class Router
    NoRouteDefined = Class.new(StandardError) do
      def initialize(topic)
        super("Subscribed to, but no route defined for #{topic}")
      end
    end

    def initialize(broker:, logger:)
      @broker = broker
      @logger = logger
      @routes = Hash.new
    end

    def add_route(topic, &block)
      @routes[topic] = block
    end

    def start!
      @logger.warn(self.class.name) { 'Nothing to subscribe to' } if topics.empty?
      @broker.subscribe(*topics)

      @broker.get do |topic, message|
        @logger.debug(self.class.name) { "#{topic}: #{message}" }
        unless @routes.has_key?(topic)
          e = NoRouteDefined.new(topic)
          @logger.error(self.class.name) { e }
          raise e
        end

        route = @routes.fetch(topic)
        @logger.debug(self.class.name) { "Found route #{route} for topic #{topic}. Invoking it now." }

        route.call(topic, message)
        @logger.debug(self.class.name) { 'Done.' }
      rescue => e
        @logger.error(self.class.name) { e }
      end
    end

    def topics
      @routes.keys
    end
  end
end
