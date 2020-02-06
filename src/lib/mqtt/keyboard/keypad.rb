require 'evdev' if RUBY_PLATFORM =~ /linux/

module MQTT
  module Keyboard
    class Keypad
      STATI = %w[released pressed repeated]

      def initialize(device:, timeout: , logger:)
        @logger = logger
        @buffer = Buffer.new(timeout: timeout, logger: logger)
        @actions = {
          :KEY_KP0        => Commands::Append.new('0', logger: logger),
          :KEY_KP1        => Commands::Append.new('1', logger: logger),
          :KEY_KP2        => Commands::Append.new('2', logger: logger),
          :KEY_KP3        => Commands::Append.new('3', logger: logger),
          :KEY_KP4        => Commands::Append.new('4', logger: logger),
          :KEY_KP5        => Commands::Append.new('5', logger: logger),
          :KEY_KP6        => Commands::Append.new('6', logger: logger),
          :KEY_KP7        => Commands::Append.new('7', logger: logger),
          :KEY_KP8        => Commands::Append.new('8', logger: logger),
          :KEY_KP9        => Commands::Append.new('9', logger: logger),
          :KEY_KPPLUS     => Commands::Append.new('+', logger: logger),
          :KEY_KPMINUS    => Commands::Append.new('-', logger: logger),
          :KEY_KPASTERISK => Commands::Append.new('*', logger: logger),
          :KEY_KPSLASH    => Commands::Append.new('/', logger: logger),
          :KEY_KPDOT      => Commands::Append.new('.', logger: logger),
          :KEY_NUMLOCK    => Commands::Print.new(logger: logger),
          :KEY_KPENTER    => Commands::DummySend.new(logger: logger),
          :KEY_BACKSPACE  => Commands::Chop.new(logger: logger),
        }
        @keyboard = Evdev.new(device)
        @logger.info(self.class.name) { "Reading events from #{@keyboard.name}" }
      end

      def register(key, command)
        @actions[key] = command
      end

      def start!
        @logger.debug(self.class.name) { "Registering handlers for #{keys}" }

        @keyboard.on(*keys) do |state, key|
          @logger.debug(self.class.name) { "Key #{key} was #{STATI[state]}" }

          case state
          when 0 # released
            command = command(key)
            @logger.info(self.class.name) { "Calling #{command} with buffer '#{@buffer}'" }
            command.call(@buffer)
          when 1 # pressed
            # ignored
          when 2 # repeated
            # ignored
          else
            raise "Don't know what to do with state #{state}"
          end
        end

        @logger.info(self.class.name) { 'Ready to accept keyboard events' }

        loop do
          begin
            @keyboard.handle_event
          rescue Evdev::AwaitEvent
            Kernel.select([@keyboard.event_channel])
            retry
          rescue Interrupt
            @logger.warn(self.class.name) { "Discarding '#{@buffer}'" } unless @buffer.empty?
            return
          end
        end
      end

      private

      def keys
        @actions.keys
      end

      def command(key)
        @actions.fetch(key)
      end
    end
  end
end
