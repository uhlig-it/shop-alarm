require_relative '../../lib/lock'

RSpec.describe ShopAlarm::Lock do
  subject(:lock) { described_class.new(unlock_code) }
  let(:unlock_code) { '12345' }

  context 'empty unlock code is typed' do
    before do
      lock.type("")
    end

    it 'publishes an "" event'
  end

  context 'correct unlock code is typed' do
    before do
      lock.type("+12345")
    end

    it 'publishes an "unlocked" event'
  end

  context 'incorrect unlock code is typed' do
    before do
      lock.type("+54321")
    end

    it 'publishes an "unlock-failed" event'
  end

  context 'correct lock code is typed' do
    before do
      lock.type("-12345")
    end

    it 'publishes an "locked" event'
  end

  context 'incorrect lock code is typed' do
    before do
      lock.type("-54321")
    end

    it 'publishes an "lock-failed" event'
  end
end
