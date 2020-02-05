require 'evdev'

class Buffer
  def initialize(timeout = 3)
    @timeout = timeout
    reset
  end

  def append(char)
    writing do
      @chars << char
    end
  end

  def chop!
    writing do
      @chars.chop!
    end
  end

  def reset
    @chars = ''
    @last_changed = Time.now
  end

  def empty?
    reading do
      @chars.empty?
    end
  end

  def to_s
    reading do
      @chars.to_s
    end
  end

  private

  # reset if needed
  def reading(&block)
    reset if Time.now - @last_changed > @timeout
    block.call
  end

  # reset if needed and DO update the last_changed timestamp
  def writing(&block)
    reading(&block)
  ensure
    @last_changed = Time.now
  end
end

class AppendCommand
  def initialize(char)
    @char = char
  end

  def call(buffer)
    buffer.append(@char)
  end
end

class SendCommand
  def initialize(mqtt)
    @mqtt = mqtt
  end

  def call(buffer)
    return if buffer.empty?
    warn "Sending #{buffer}"
    buffer.reset
  end
end

class PrintCommand
  def call(buffer)
    if buffer.empty?
      warn 'Current buffer is empty'
    else
      warn "Current buffer is #{buffer}"
    end
  end
end

class ChopCommand
  def call(buffer)
    buffer.chop!
  end
end

class Keypad
  ACTIONS = {
    :KEY_KP0        => AppendCommand.new('0'),
    :KEY_KP1        => AppendCommand.new('1'),
    :KEY_KP2        => AppendCommand.new('2'),
    :KEY_KP3        => AppendCommand.new('3'),
    :KEY_KP4        => AppendCommand.new('4'),
    :KEY_KP5        => AppendCommand.new('5'),
    :KEY_KP6        => AppendCommand.new('6'),
    :KEY_KP7        => AppendCommand.new('7'),
    :KEY_KP8        => AppendCommand.new('8'),
    :KEY_KP9        => AppendCommand.new('9'),
    :KEY_KPPLUS     => AppendCommand.new('+'),
    :KEY_KPMINUS    => AppendCommand.new('-'),
    :KEY_KPASTERISK => AppendCommand.new('*'),
    :KEY_KPSLASH    => AppendCommand.new('/'),
    :KEY_KPDOT      => AppendCommand.new('.'),
    :KEY_NUMLOCK    => PrintCommand.new,
    :KEY_KPENTER    => SendCommand.new(nil),
    :KEY_BACKSPACE  => ChopCommand.new,
  }

  def initialize(device:, timeout: 3)
    @keyboard = Evdev.new(device)
    @buffer = Buffer.new(timeout)

    @keyboard.on(*ACTIONS.keys) do |state, key|
      case state
      when 0 # released
        ACTIONS[key].call(@buffer)
      when 1 # pressed
        # ignored
      when 2 # repeated
        # ignored
      else
        raise "Don't know what to do with state #{state}"
      end
    end
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

kp = Keypad.new(device: '/dev/input/event0')
warn 'Ready'
kp.start!

__END__

TODO

* Command line args
* MQTT
* Tests?
* Can we [blink the LED](https://hewner.github.io/2006/08/21/evdev-for-ruby-with-morse-code/)?
