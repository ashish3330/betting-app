"use client";

import { useState } from "react";
import Link from "next/link";

const quizQuestions = [
  "Do you spend more money on gambling than you can afford to lose?",
  "Do you feel the need to gamble with increasing amounts of money?",
  "Have you tried to cut back on gambling but found it difficult?",
  "Do you feel restless or irritable when trying to stop gambling?",
  "Do you gamble to escape problems or relieve feelings of anxiety or depression?",
];

export default function ResponsibleGamblingPage() {
  const [quizAnswers, setQuizAnswers] = useState<(boolean | null)[]>(
    Array(quizQuestions.length).fill(null)
  );
  const [quizSubmitted, setQuizSubmitted] = useState(false);

  const yesCount = quizAnswers.filter((a) => a === true).length;
  const allAnswered = quizAnswers.every((a) => a !== null);

  function handleQuizAnswer(index: number, answer: boolean) {
    const newAnswers = [...quizAnswers];
    newAnswers[index] = answer;
    setQuizAnswers(newAnswers);
  }

  function handleSubmitQuiz() {
    if (allAnswered) setQuizSubmitted(true);
  }

  return (
    <div className="max-w-4xl mx-auto px-4 py-8 space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Responsible Gambling</h1>
        <p className="text-xs text-gray-500 mt-1">
          Your well-being is our priority. Gambling should be fun, not a source of stress.
        </p>
      </div>

      {/* What is Responsible Gambling */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">What is Responsible Gambling?</h2>
        <div className="text-sm text-gray-400 leading-relaxed space-y-2">
          <p>
            Responsible gambling means enjoying betting as a form of entertainment while maintaining
            full control over your time and money. It means understanding that gambling always involves
            risk and that the odds are never guaranteed.
          </p>
          <p>
            At 3XBet, we provide tools and resources to help you gamble responsibly. We
            encourage all our users to set limits, take breaks, and seek help if gambling stops being
            fun.
          </p>
        </div>
      </section>

      {/* Tips for Safe Betting */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Tips for Safe Betting</h2>
        <ul className="text-sm text-gray-400 leading-relaxed list-disc list-inside space-y-1">
          <li>Set a budget before you start and stick to it.</li>
          <li>Never chase your losses &mdash; accept them and move on.</li>
          <li>Set time limits for your gambling sessions.</li>
          <li>Do not gamble when you are upset, stressed, or under the influence of alcohol.</li>
          <li>Take regular breaks from gambling.</li>
          <li>Never borrow money to gamble.</li>
          <li>Balance gambling with other hobbies and activities.</li>
          <li>Remember: gambling is entertainment, not a way to make money.</li>
        </ul>
      </section>

      {/* Self-Assessment Quiz */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Self-Assessment Quiz</h2>
        <p className="text-sm text-gray-400 mb-4">
          Answer these questions honestly to assess whether your gambling habits may be a concern.
        </p>
        <div className="space-y-4">
          {quizQuestions.map((question, i) => (
            <div key={i} className="flex items-start gap-3">
              <span className="text-xs text-gray-400 font-mono mt-0.5 w-5 flex-shrink-0">{i + 1}.</span>
              <div className="flex-1">
                <p className="text-sm text-gray-300 mb-2">{question}</p>
                <div className="flex gap-2">
                  <button
                    onClick={() => handleQuizAnswer(i, true)}
                    disabled={quizSubmitted}
                    className={`px-4 py-1 rounded text-xs font-medium transition ${
                      quizAnswers[i] === true
                        ? "bg-loss/20 text-loss border border-loss"
                        : "bg-surface-light text-gray-400 border border-gray-700 hover:border-gray-600"
                    } disabled:opacity-60`}
                  >
                    Yes
                  </button>
                  <button
                    onClick={() => handleQuizAnswer(i, false)}
                    disabled={quizSubmitted}
                    className={`px-4 py-1 rounded text-xs font-medium transition ${
                      quizAnswers[i] === false
                        ? "bg-profit/20 text-profit border border-profit"
                        : "bg-surface-light text-gray-400 border border-gray-700 hover:border-gray-600"
                    } disabled:opacity-60`}
                  >
                    No
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>

        {!quizSubmitted && (
          <button
            onClick={handleSubmitQuiz}
            disabled={!allAnswered}
            className="mt-4 bg-lotus hover:bg-lotus-light text-white text-xs px-6 py-2 rounded-lg transition disabled:opacity-40"
          >
            Submit Assessment
          </button>
        )}

        {quizSubmitted && (
          <div
            className={`mt-4 p-4 rounded-lg border ${
              yesCount >= 3
                ? "bg-loss/10 border-loss/40"
                : yesCount >= 1
                ? "bg-yellow-500/10 border-yellow-500/40"
                : "bg-profit/10 border-profit/40"
            }`}
          >
            <p
              className={`text-sm font-medium ${
                yesCount >= 3
                  ? "text-loss"
                  : yesCount >= 1
                  ? "text-yellow-500"
                  : "text-profit"
              }`}
            >
              {yesCount >= 3
                ? "Your answers suggest you may be at risk. We strongly recommend seeking help and setting strict limits."
                : yesCount >= 1
                ? "Some of your habits may need attention. Consider setting deposit and session limits."
                : "Your gambling habits appear to be under control. Keep it up!"}
            </p>
            {yesCount >= 1 && (
              <p className="text-xs text-gray-400 mt-2">
                You answered &quot;Yes&quot; to {yesCount} out of {quizQuestions.length} questions.
                Please review the resources below.
              </p>
            )}
          </div>
        )}
      </section>

      {/* Set Limits */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Set Your Limits</h2>
        <p className="text-sm text-gray-400 mb-4">
          Take control of your gambling by setting deposit, loss, and session time limits.
        </p>
        <div className="flex flex-wrap gap-3">
          <Link
            href="/account"
            className="bg-lotus hover:bg-lotus-light text-white text-xs px-5 py-2 rounded-lg transition"
          >
            Set Deposit Limits
          </Link>
          <Link
            href="/account"
            className="bg-surface-light hover:bg-surface-lighter text-white text-xs px-5 py-2 rounded-lg border border-gray-700 transition"
          >
            Set Loss Limits
          </Link>
          <Link
            href="/account"
            className="bg-surface-light hover:bg-surface-lighter text-white text-xs px-5 py-2 rounded-lg border border-gray-700 transition"
          >
            Set Session Limits
          </Link>
        </div>
      </section>

      {/* Self-Exclusion */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Self-Exclusion</h2>
        <p className="text-sm text-gray-400 mb-4">
          If you feel you need a break from gambling, you can temporarily or permanently exclude yourself
          from the Platform. During self-exclusion, you will not be able to place bets or access your
          account.
        </p>
        <Link
          href="/account"
          className="bg-loss/20 hover:bg-loss/30 text-loss text-xs px-5 py-2 rounded-lg border border-loss/40 transition"
        >
          Self-Exclude via Account Settings
        </Link>
      </section>

      {/* Warning Signs */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Warning Signs of Problem Gambling</h2>
        <ul className="text-sm text-gray-400 leading-relaxed list-disc list-inside space-y-1">
          <li>Spending more money or time on gambling than you intended.</li>
          <li>Feeling the need to be secretive about your gambling.</li>
          <li>Having trouble controlling or stopping gambling.</li>
          <li>Gambling even when you cannot afford it or borrowing money to gamble.</li>
          <li>Neglecting work, family, or personal responsibilities due to gambling.</li>
          <li>Feeling anxious, irritable, or depressed when not gambling.</li>
          <li>Chasing losses by increasing bets to recover money.</li>
          <li>Lying to family or friends about how much you gamble.</li>
        </ul>
      </section>

      {/* Helpline Numbers */}
      <section className="bg-surface rounded-xl border border-gray-800 p-5">
        <h2 className="text-sm font-bold text-white mb-3">Helpline &amp; Support</h2>
        <p className="text-sm text-gray-400 mb-4">
          If you or someone you know is struggling with problem gambling, please reach out to these
          organisations for confidential support:
        </p>
        <div className="space-y-3">
          <div className="bg-surface-light rounded-lg p-4 border border-gray-700">
            <h3 className="text-sm font-semibold text-white">Gambling Helpline India</h3>
            <p className="text-xs text-gray-400 mt-1">Free, confidential helpline for gambling-related issues.</p>
            <p className="text-sm text-lotus font-mono mt-2">1800-599-0019 (Toll-free)</p>
          </div>
          <div className="bg-surface-light rounded-lg p-4 border border-gray-700">
            <h3 className="text-sm font-semibold text-white">Gamblers Anonymous International</h3>
            <p className="text-xs text-gray-400 mt-1">A fellowship of men and women who share their experience, strength, and hope with each other.</p>
            <p className="text-sm text-lotus font-mono mt-2">www.gamblersanonymous.org</p>
          </div>
          <div className="bg-surface-light rounded-lg p-4 border border-gray-700">
            <h3 className="text-sm font-semibold text-white">National Council on Problem Gambling</h3>
            <p className="text-xs text-gray-400 mt-1">Resources and referrals for those affected by problem gambling.</p>
            <p className="text-sm text-lotus font-mono mt-2">1-800-522-4700</p>
          </div>
        </div>
      </section>
    </div>
  );
}
