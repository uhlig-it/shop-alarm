# frozen_string_literal: true

require 'mqtt'
require_relative '../../lib/mqtt/router'
require 'rspec/eventually'
require 'logger'

class BrokerController
  def start!
    @pid = fork do
      exec 'mosquitto'
    end

    Process.detach(@pid)
  end

  def stop!
    Process.kill("TERM", @pid)
  end
end

RSpec.describe MQTT::Router do
  subject(:router) { described_class.new(broker: broker, logger: logger) }
  let(:broker) { MQTT::Client.new('mqtt://localhost:1883/rspec') }
  let(:received_messages) { Hash.new }
  let(:logger) { Logger.new('/dev/null') }

  before do
    @broker_controller = BrokerController.new
    @broker_controller.start!

    begin
      broker.connect
    rescue Errno::ECONNREFUSED
      warn "Broker not ready yet"
      retry
    end
  end

  after {
    broker.disconnect
    @broker_controller.stop!
  }

  context 'no route' do
    it 'does not receive messages' do
      start_router
      broker.publish('test', 'welcome')
      expect(received_messages).to be_empty
    end
  end

  context 'simple topic' do
    before do
      router.add_route('test') do |broker, topic, message|
        received_messages[topic] = message
      end
    end

    it 'receives the message' do
      start_router
      broker.publish('test', 'welcome')
      expect { received_messages['test'] }.to eventually eq 'welcome'
    end
  end

  xcontext 'single-level wildcard' do
    before do
      router.add_route('foo/+/bar') do |broker, topic, message|
        received_messages[topic] = message
      end
    end

    it 'receives the message' do
      start_router
      broker.publish('foo/some/bar', 'it works!')
      expect { received_messages['foo/some/bar'] }.to eventually eq 'it works!'
    end
  end

  xcontext 'multi-level wildcard'

  private

  def start_router
    Thread.new { router.start! }
    sleep 1 # needs some time to get up
  end
end

__END__

Ideas:
* # is a simple ends_with?('#')
* +: split topic at `+`, and check if it starts with the first part and ends with the second.
