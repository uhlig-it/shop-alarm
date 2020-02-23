require_relative 'base'

module MQTT
  module Keyboard
    module Commands
      class Append < Base
        def initialize(char, logger:)
          super(logger: logger)
          @char = char
        end

        def call(buffer)
          debug "Appending #{@char} to '#{buffer}'"
          buffer.append(@char)
        end
      end
    end
  end
end
