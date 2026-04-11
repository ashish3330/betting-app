"use client";

export default function PrivacyPage() {
  return (
    <div className="max-w-4xl mx-auto px-4 py-8 space-y-8">
      <div>
        <h1 className="text-2xl font-bold text-white">Privacy Policy</h1>
        <p className="text-xs text-gray-500 mt-1">Last updated: 1 April 2026</p>
      </div>

      {/* 1. Information We Collect */}
      <Section num={1} title="Information We Collect">
        <p className="mb-2">We collect the following types of information when you use Lotus Exchange:</p>
        <ul className="list-disc list-inside space-y-1">
          <li><strong>Personal Information:</strong> Name, email address, phone number, date of birth, and residential address provided during registration.</li>
          <li><strong>Identity Documents:</strong> PAN Card, Aadhaar Card, bank statements, and other documents submitted for KYC verification.</li>
          <li><strong>Financial Information:</strong> UPI IDs, bank account details, cryptocurrency wallet addresses used for deposits and withdrawals.</li>
          <li><strong>Usage Data:</strong> Betting history, login times, IP addresses, device information, browser type, and pages visited.</li>
          <li><strong>Communication Data:</strong> Messages sent to customer support and feedback provided.</li>
        </ul>
      </Section>

      {/* 2. How We Use Your Data */}
      <Section num={2} title="How We Use Your Data">
        <ul className="list-disc list-inside space-y-1">
          <li>To create and manage your account on the Platform.</li>
          <li>To process deposits, withdrawals, and betting transactions.</li>
          <li>To verify your identity and comply with KYC and anti-money laundering regulations.</li>
          <li>To detect and prevent fraud, money laundering, and other illegal activities.</li>
          <li>To provide customer support and respond to your enquiries.</li>
          <li>To send important account notifications (transaction confirmations, security alerts).</li>
          <li>To improve our services, analyse usage patterns, and develop new features.</li>
          <li>To enforce our Terms &amp; Conditions and protect the integrity of the Platform.</li>
        </ul>
      </Section>

      {/* 3. Data Security */}
      <Section num={3} title="Data Security">
        <p className="mb-2">We take the security of your data seriously and employ industry-leading measures:</p>
        <ul className="list-disc list-inside space-y-1">
          <li><strong>AES-256 Encryption:</strong> All sensitive data is encrypted at rest and in transit using AES-256 encryption.</li>
          <li><strong>TLS 1.3:</strong> All communications between your browser and our servers are secured with TLS 1.3.</li>
          <li><strong>Secure Password Storage:</strong> Passwords are hashed using Argon2id with unique salts.</li>
          <li><strong>Access Controls:</strong> Strict role-based access controls limit who can access your data within our organisation.</li>
          <li><strong>Regular Audits:</strong> We conduct regular security audits and penetration testing.</li>
          <li><strong>Token-based Authentication:</strong> JWT tokens with Ed25519 signatures for secure session management.</li>
        </ul>
      </Section>

      {/* 4. Cookies */}
      <Section num={4} title="Cookies">
        <p className="mb-2">We use cookies and similar technologies to enhance your experience:</p>
        <ul className="list-disc list-inside space-y-1">
          <li><strong>Essential Cookies:</strong> Required for the Platform to function (authentication, session management).</li>
          <li><strong>Preference Cookies:</strong> Remember your settings such as language and display preferences.</li>
          <li><strong>Analytics Cookies:</strong> Help us understand how users interact with the Platform to improve our services.</li>
          <li>You can manage cookie preferences through your browser settings. Disabling essential cookies may affect Platform functionality.</li>
        </ul>
      </Section>

      {/* 5. Third Party Services */}
      <Section num={5} title="Third Party Services">
        <ul className="list-disc list-inside space-y-1">
          <li><strong>Payment Processors:</strong> We use third-party payment processors for UPI and cryptocurrency transactions. These processors have their own privacy policies.</li>
          <li><strong>KYC Verification:</strong> Identity verification may be conducted through third-party KYC service providers.</li>
          <li><strong>Analytics:</strong> We may use analytics services to understand Platform usage patterns.</li>
          <li><strong>Cloud Infrastructure:</strong> Our services are hosted on secure cloud infrastructure with data centres located in compliance with applicable regulations.</li>
          <li>We do not sell, rent, or trade your personal data to any third party for marketing purposes.</li>
        </ul>
      </Section>

      {/* 6. Data Retention */}
      <Section num={6} title="Data Retention">
        <ul className="list-disc list-inside space-y-1">
          <li>Account data is retained for the duration of your account and for 5 years after closure, as required by regulations.</li>
          <li>Transaction records are retained for a minimum of 7 years for compliance purposes.</li>
          <li>KYC documents are retained for 5 years after account closure.</li>
          <li>Usage logs are retained for 12 months and then anonymised.</li>
          <li>You may request earlier deletion of non-essential data, subject to our regulatory obligations.</li>
        </ul>
      </Section>

      {/* 7. Your Rights */}
      <Section num={7} title="Your Rights">
        <p className="mb-2">You have the following rights regarding your personal data:</p>
        <ul className="list-disc list-inside space-y-1">
          <li><strong>Right of Access:</strong> Request a copy of all personal data we hold about you.</li>
          <li><strong>Right to Rectification:</strong> Request correction of inaccurate or incomplete data.</li>
          <li><strong>Right to Erasure:</strong> Request deletion of your data, subject to regulatory retention requirements.</li>
          <li><strong>Right to Restrict Processing:</strong> Request that we limit how we use your data.</li>
          <li><strong>Right to Data Portability:</strong> Request your data in a structured, machine-readable format.</li>
          <li><strong>Right to Object:</strong> Object to processing of your data for specific purposes.</li>
        </ul>
        <p className="mt-2">To exercise any of these rights, please contact our Data Protection Officer at the address below.</p>
      </Section>

      {/* 8. Contact Information */}
      <Section num={8} title="Contact Information">
        <p className="mb-2">For any privacy-related enquiries or to exercise your data rights, please contact us:</p>
        <ul className="space-y-1">
          <li><strong>Email:</strong> privacy@lotusexchange.com</li>
          <li><strong>Data Protection Officer:</strong> dpo@lotusexchange.com</li>
          <li><strong>Support:</strong> support@lotusexchange.com</li>
        </ul>
        <p className="mt-3">
          We aim to respond to all data requests within 30 days. This Privacy Policy may be updated from
          time to time. We will notify you of material changes via email or through the Platform.
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
