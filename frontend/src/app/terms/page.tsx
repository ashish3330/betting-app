"use client";

export default function TermsPage() {
  return (
    <div className="max-w-4xl mx-auto px-4 py-8 space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Terms &amp; Conditions</h1>
        <p className="text-xs text-gray-500 mt-1">Last updated: 1 April 2026</p>
      </div>

      {/* 1. Introduction */}
      <Section num={1} title="Introduction">
        <p>
          Welcome to 3XBet (&quot;the Platform&quot;, &quot;we&quot;, &quot;us&quot;, &quot;our&quot;). By
          accessing or using our services, you agree to be bound by these Terms &amp; Conditions. If you
          do not agree, you must not use the Platform. These terms constitute a legally binding agreement
          between you and 3XBet.
        </p>
      </Section>

      {/* 2. Definitions */}
      <Section num={2} title="Definitions">
        <ul className="list-disc list-inside space-y-1">
          <li><strong>&quot;Account&quot;</strong> means a registered user account on the Platform.</li>
          <li><strong>&quot;Back Bet&quot;</strong> means a bet placed in favour of an outcome occurring.</li>
          <li><strong>&quot;Lay Bet&quot;</strong> means a bet placed against an outcome occurring.</li>
          <li><strong>&quot;Exchange&quot;</strong> means the peer-to-peer betting marketplace operated by us.</li>
          <li><strong>&quot;Market&quot;</strong> means a specific betting event or proposition listed on the Exchange.</li>
          <li><strong>&quot;Commission&quot;</strong> means the fee charged by 3XBet on net winnings.</li>
          <li><strong>&quot;KYC&quot;</strong> means Know Your Customer verification procedures.</li>
          <li><strong>&quot;Wallet&quot;</strong> means your digital balance held on the Platform.</li>
        </ul>
      </Section>

      {/* 3. Account Registration */}
      <Section num={3} title="Account Registration">
        <ul className="list-disc list-inside space-y-1">
          <li>You must be at least <strong>18 years of age</strong> to register and use this Platform.</li>
          <li>Each person may hold only <strong>one account</strong>. Multiple accounts will be suspended.</li>
          <li>You must provide accurate, current, and complete information during registration.</li>
          <li>You are responsible for maintaining the confidentiality of your login credentials.</li>
          <li>We reserve the right to verify your identity (KYC) before processing withdrawals.</li>
          <li>Accounts found to be operated by minors will be immediately closed and all funds forfeited to the appropriate authorities.</li>
        </ul>
      </Section>

      {/* 4. Deposits and Withdrawals */}
      <Section num={4} title="Deposits and Withdrawals">
        <ul className="list-disc list-inside space-y-1">
          <li>Deposits can be made via UPI, bank transfer, or supported cryptocurrency.</li>
          <li>Minimum deposit amounts apply and are displayed on the deposit page.</li>
          <li>Withdrawals require completed KYC verification.</li>
          <li>Withdrawal requests are processed within 24&ndash;48 hours on business days.</li>
          <li>We reserve the right to request additional documentation before processing large withdrawals.</li>
          <li>Funds must be wagered at least once before withdrawal to prevent money laundering.</li>
        </ul>
      </Section>

      {/* 5. Betting Rules */}
      <Section num={5} title="Betting Rules">
        <p className="mb-2">
          3XBet operates as a peer-to-peer betting exchange. Unlike traditional bookmakers, you
          bet against other users.
        </p>
        <ul className="list-disc list-inside space-y-1">
          <li><strong>Back betting:</strong> You bet that an outcome <em>will</em> happen. Your potential profit is (odds - 1) x stake.</li>
          <li><strong>Lay betting:</strong> You bet that an outcome <em>will not</em> happen. Your liability is (odds - 1) x stake.</li>
          <li>Bets are matched against opposing bets in the order book. Unmatched portions remain open until matched or cancelled.</li>
          <li>All matched bets are final and cannot be cancelled.</li>
          <li>Minimum and maximum stake limits apply per market.</li>
          <li>We reserve the right to void bets in cases of obvious errors, technical malfunctions, or fraudulent activity.</li>
        </ul>
      </Section>

      {/* 6. Market Settlement */}
      <Section num={6} title="Market Settlement">
        <ul className="list-disc list-inside space-y-1">
          <li>Markets are settled based on the official result from the relevant governing body.</li>
          <li>Settlement typically occurs within minutes of the event conclusion.</li>
          <li>In cases of dispute, our settlement team&apos;s decision is final.</li>
          <li>Abandoned or postponed events may be voided at our discretion.</li>
          <li>Dead heat rules apply where applicable.</li>
        </ul>
      </Section>

      {/* 7. Commission */}
      <Section num={7} title="Commission">
        <ul className="list-disc list-inside space-y-1">
          <li>3XBet charges a commission on net winnings per market.</li>
          <li>The standard commission rate is displayed in your account settings.</li>
          <li>Commission is deducted automatically upon market settlement.</li>
          <li>Commission rates may vary by sport, market type, or account level.</li>
        </ul>
      </Section>

      {/* 8. Responsible Gambling */}
      <Section num={8} title="Responsible Gambling">
        <ul className="list-disc list-inside space-y-1">
          <li>We are committed to promoting responsible gambling.</li>
          <li>You may set daily deposit limits, loss limits, and session time limits via your account settings.</li>
          <li>Self-exclusion options are available for periods of 24 hours, 7 days, 30 days, or permanently.</li>
          <li>If you believe you have a gambling problem, please visit our <a href="/responsible-gambling" className="text-lotus hover:underline">Responsible Gambling</a> page.</li>
          <li>We reserve the right to restrict or close accounts where we suspect problem gambling behaviour.</li>
        </ul>
      </Section>

      {/* 9. Privacy and Data */}
      <Section num={9} title="Privacy and Data">
        <ul className="list-disc list-inside space-y-1">
          <li>Your personal data is processed in accordance with our <a href="/privacy" className="text-lotus hover:underline">Privacy Policy</a>.</li>
          <li>We use AES-256 encryption to protect data in transit and at rest.</li>
          <li>We do not sell your personal data to third parties.</li>
          <li>You have the right to request access to, correction, or deletion of your personal data.</li>
        </ul>
      </Section>

      {/* 10. Account Closure */}
      <Section num={10} title="Account Closure">
        <ul className="list-disc list-inside space-y-1">
          <li>You may close your account at any time by contacting support.</li>
          <li>Outstanding bets will be settled before any remaining balance is returned.</li>
          <li>We reserve the right to close or suspend accounts that breach these terms.</li>
          <li>Funds in closed accounts will be returned to the original deposit method where possible.</li>
        </ul>
      </Section>

      {/* 11. Dispute Resolution */}
      <Section num={11} title="Dispute Resolution">
        <ul className="list-disc list-inside space-y-1">
          <li>Any disputes should first be raised with our customer support team.</li>
          <li>We aim to resolve all disputes within 14 business days.</li>
          <li>If a dispute cannot be resolved amicably, it shall be referred to binding arbitration.</li>
          <li>The arbitration shall be conducted in accordance with the rules of the applicable jurisdiction.</li>
        </ul>
      </Section>

      {/* 12. Governing Law */}
      <Section num={12} title="Governing Law">
        <p>
          These Terms &amp; Conditions shall be governed by and construed in accordance with the laws of
          the applicable jurisdiction. Any legal proceedings arising out of or in connection with these
          terms shall be subject to the exclusive jurisdiction of the competent courts.
        </p>
        <p className="mt-2">
          By using 3XBet, you acknowledge that you have read, understood, and agreed to these
          Terms &amp; Conditions in their entirety.
        </p>
      </Section>
    </div>
  );
}

function Section({
  num,
  title,
  children,
}: {
  num: number;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section className="bg-surface rounded-xl border border-gray-800 p-5">
      <h2 className="text-sm font-bold text-white mb-3">
        {num}. {title}
      </h2>
      <div className="text-sm text-gray-400 leading-relaxed">{children}</div>
    </section>
  );
}
