require 'evdev'

buffer = ''

KEYPAD = {
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
  :KEY_NUMLOCK    => lambda { warn "Current buffer is #{buffer}" },
  :KEY_KPENTER    => lambda { warn "Sending #{buffer}"; buffer = '' },
  :KEY_BACKSPACE  => lambda { warn 'TODO backspace' },
}

keyboard = Evdev.new('/dev/input/event0')

keyboard.on(*KEYPAD.keys) do |state, key|
  case state
  when 0
    action = KEYPAD[key]
    action.call rescue buffer << action
  when 1
    # warn "Pressed #{KEYPAD[key]}"
  when 2
    # warn "Woah, slow down with that #{key}!"
  else
    raise "What? #{state}"
  end
end

warn 'Ready'

loop do
  begin
    keyboard.handle_event
  rescue Evdev::AwaitEvent
    Kernel.select([keyboard.event_channel])
    retry
  end
end

__END__

TODO

* Encapsulate buffer in a class
* Timeout
* Backspace
* Do not send empty buffer
* Command line args
* MQTT
* Tests?
* Can we [blink the LED](https://hewner.github.io/2006/08/21/evdev-for-ruby-with-morse-code/)?
