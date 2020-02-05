require 'evdev'
require 'forwardable'

class Buffer
  extend Forwardable

  def_delegator :@chars, :to_s
  def_delegator :@chars, :chop!
  def_delegator :@chars, :empty?

  def initialize
    reset
  end

  def append(char)
    @chars << char
  end

  def reset
    @chars = ''
  end
end

class Keypad
  ACTIONS = {
    :KEY_KP0        => '0',
    :KEY_KP1        => '1',
    :KEY_KP2        => '2',
    :KEY_KP3        => '3',
    :KEY_KP4        => '4',
    :KEY_KP5        => '5',
    :KEY_KP6        => '6',
    :KEY_KP7        => '7',
    :KEY_KP8        => '8',
    :KEY_KP9        => '9',
    :KEY_KPPLUS     => '+',
    :KEY_KPMINUS    => '-',
    :KEY_KPASTERISK => '*',
    :KEY_KPSLASH    => '/',
    :KEY_KPDOT      => '.',
    :KEY_NUMLOCK    => lambda { |buffer| warn "Current buffer is #{buffer}" },
    :KEY_KPENTER    => lambda { |buffer| warn "Sending #{buffer}"; buffer.reset },
    :KEY_BACKSPACE  => lambda { |buffer| buffer.chop! },
  }

  def initialize(device)
    @keyboard = Evdev.new(device)

    @keyboard.on(*ACTIONS.keys) do |state, key|
      case state
      when 0
        action = ACTIONS[key]

        if action.respond_to?(:call)
          action.call(@buffer)
        else
          @buffer.append(action)
        end
      when 1
        # warn "Pressed #{ACTIONS[key]}"
      when 2
        # warn "Woah, slow down with that #{key}!"
      else
        raise "What? #{state}"
      end
    end

    @buffer = Buffer.new
  end

  def start!
    loop do
      begin
        @keyboard.handle_event
      rescue Evdev::AwaitEvent
        Kernel.select([@keyboard.event_channel])
        retry
      rescue Interrupt
        warn "Discarding #{@buffer}" unless @buffer.empty?
        return
      end
    end
  end
end

kp = Keypad.new('/dev/input/event0')
warn 'Ready'
kp.start!

__END__

TODO

* Timeout
* Do not send empty buffer
* Command line args
* MQTT
* Tests?
* Can we [blink the LED](https://hewner.github.io/2006/08/21/evdev-for-ruby-with-morse-code/)?
