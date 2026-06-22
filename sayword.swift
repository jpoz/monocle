// sayword speaks the UTF-8 text read from stdin with AVSpeechSynthesizer and,
// as each word is spoken, prints its character range into the spoken string to
// stdout as "W <loc> <len>" (UTF-16 offsets, NSString semantics). It prints "D"
// when speech finishes. Reporting the synthesizer's real word boundaries lets
// the caller highlight the focal word in exact step with the audio instead of
// estimating per-word timing, which drifts on numbers, abbreviations, and the
// engine's own phrasing pauses.
//
// AVSpeechSynthesizer is used over the older NSSpeechSynthesizer because the
// latter silently drops its willSpeakWord callbacks for phrases containing
// certain punctuation (notably the em dash), freezing the highlight mid-
// sentence. AVSpeechSynthesizer reports every word reliably.
//
// Usage:
//   sayword --voices          list installed voices as "<id>\t<name>\t<lang>"
//   sayword <rate> [voiceID]  speak stdin; rate is AVSpeechUtterance's 0...1
//                             scale (~0.5 natural), voiceID selects a voice
import AVFoundation
import Foundation

final class Speaker: NSObject, AVSpeechSynthesizerDelegate {
    let synth = AVSpeechSynthesizer()
    let out = FileHandle.standardOutput

    func emit(_ s: String) {
        out.write(Data((s + "\n").utf8))
    }

    func speak(_ text: String, rate: Float, voiceID: String) {
        synth.delegate = self
        let u = AVSpeechUtterance(string: text)
        if rate > 0 {
            u.rate = min(max(rate, AVSpeechUtteranceMinimumSpeechRate), AVSpeechUtteranceMaximumSpeechRate)
        }
        if !voiceID.isEmpty, let v = AVSpeechSynthesisVoice(identifier: voiceID) {
            u.voice = v
        }
        synth.speak(u)
    }

    func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, willSpeakRangeOfSpeechString characterRange: NSRange, utterance: AVSpeechUtterance) {
        emit("W \(characterRange.location) \(characterRange.length)")
    }

    func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didFinish utterance: AVSpeechUtterance) {
        emit("D")
        exit(0)
    }

    func speechSynthesizer(_ synthesizer: AVSpeechSynthesizer, didCancel utterance: AVSpeechUtterance) {
        emit("D")
        exit(0)
    }
}

let args = CommandLine.arguments

if args.count > 1, args[1] == "--voices" {
    for v in AVSpeechSynthesisVoice.speechVoices() {
        print("\(v.identifier)\t\(v.name)\t\(v.language)")
    }
    exit(0)
}

var rate: Float = 0
if args.count > 1, let r = Float(args[1]) { rate = r }
let voiceID = args.count > 2 ? args[2] : ""

let data = FileHandle.standardInput.readDataToEndOfFile()
let text = String(data: data, encoding: .utf8) ?? ""

let speaker = Speaker()
speaker.speak(text, rate: rate, voiceID: voiceID)
RunLoop.main.run()
