import { Link } from "react-router";
import { ArrowLeft } from "lucide-react";
import { useI18n, useT } from "@/lib/i18n";

/**
 * Privacy policy.
 *
 * This is a real page rather than a placeholder link because the sign-in form now asks the user to agree
 * to it: a checkbox pointing at a 404 is worse than no checkbox, since it records consent to nothing. It
 * is reachable without an account (it is outside /dashboard) because the consent happens before sign-in.
 *
 * The copy is deliberately specific about what is stored and who else sees it, which is also the honest
 * list: the phone number, the SMS provider that delivers the code, the model providers that answer
 * questions, and the click telemetry the product exists to collect.
 */
export default function PrivacyPage() {
  const t = useT();
  const { locale } = useI18n();
  const isZh = locale === "zh";

  return (
    <div className="min-h-dvh bg-subtle px-4 py-10">
      <div className="mx-auto max-w-3xl">
        <Link
          to="/login"
          className="inline-flex items-center gap-1.5 text-sm text-brand-ink hover:text-brand"
        >
          <ArrowLeft className="size-4" />
          {t("返回登录")}
        </Link>

        <article className="mt-6 rounded-xl border border-line bg-canvas p-6 shadow-sm sm:p-8">
          <h1 className="text-2xl font-bold text-strong">{t("隐私政策")}</h1>
          <p className="mt-2 text-sm text-muted">{t("最近更新：2026 年 1 月")}</p>

          {isZh ? <ChinesePolicy /> : <EnglishPolicy />}
        </article>
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="mt-6">
      <h2 className="text-base font-semibold text-strong">{title}</h2>
      <div className="mt-2 space-y-2 text-sm leading-relaxed text-body">{children}</div>
    </section>
  );
}

/** A list item with a label, used for the "what we store" table. */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <li className="flex flex-col gap-0.5 sm:flex-row sm:gap-2">
      <span className="shrink-0 font-medium text-strong sm:w-32">{label}</span>
      <span>{children}</span>
    </li>
  );
}

function ChinesePolicy() {
  return (
    <>
      <Section title="一、我们收集哪些信息">
        <ul className="space-y-2">
          <Row label="账号信息">手机号。它是登录凭据，也是我们唯一用于识别你的字段。</Row>
          <Row label="登录记录">
            最近一次登录时间与来源 IP。用于风控与账号安全，不用于画像或广告。
          </Row>
          <Row label="短信验证码">
            验证码只保存 SHA-256 摘要，5 分钟后失效、一次有效，明文不落库、不写日志。
          </Row>
          <Row label="短链与点击数据">
            你创建的链接，以及每次访问产生的点击记录：时间、来源 IP、User-Agent、Referer、由
            User-Agent 推断的平台与地区。这是短链统计功能的基础。
          </Row>
          <Row label="AI 对话">
            你在「AI 助手」里发送的消息、模型的回复，以及会话所属的用户 ID。用于在下次进入页面时
            续上会话。
          </Row>
          <Row label="访问日志">
            服务器与反向代理记录请求路径、状态码与来源 IP，用于排障和阻止滥用。
          </Row>
        </ul>
      </Section>

      <Section title="二、信息如何被使用">
        <p>仅用于提供本服务本身：登录鉴权、短链跳转、点击统计、AI 助手问答，以及防止短信接口被滥用。</p>
        <p>我们不会出售、出租你的个人信息，也不会把它用于本服务之外的广告投放。</p>
      </Section>

      <Section title="三、会与哪些第三方共享">
        <ul className="space-y-2">
          <Row label="阿里云短信">
            为下发登录验证码，我们会将你的手机号与验证码内容发送给阿里云短信服务。
          </Row>
          <Row label="DeepSeek">
            你在 AI 助手输入的内容会发送给 DeepSeek 以生成回复。
          </Row>
          <Row label="服务器与数据库">
            数据存放在本服务自有的服务器与数据库中，不向其他方开放。
          </Row>
        </ul>
        <p>除法律法规要求或上述必要的数据处理外，我们不会向第三方提供你的信息。</p>
      </Section>

      <Section title="四、短信与频率限制">
        <p>
          为保护你的手机号不被骚扰、也为了避免短信费用被恶意消耗，发送验证码前需要通过图形验证码，
          并受到三重限制：同一手机号 60 秒内只能发送一次、每天最多 10 条；同一 IP 每小时最多 10
          条、每天最多 30 条。
        </p>
      </Section>

      <Section title="五、保存期限">
        <p>
          账号信息在账号存续期间保存。验证码在 5 分钟后失效、一小时内被清理。点击记录与访问日志默认
          保留 180 天。你可以随时在「设置」中删除你创建的链接。
        </p>
      </Section>

      <Section title="六、你的权利">
        <p>
          你可以随时在「设置」中查看你的手机号，也可以删除自己创建的链接、文件夹、标签
          与 API Token。如需注销账号或导出数据，请通过下方邮箱联系我们，我们会在 15 个工作日内处理。
        </p>
      </Section>

      <Section title="七、未成年人">
        <p>本服务不面向 14 周岁以下的儿童。若你未满 14 周岁，请在监护人陪同下使用。</p>
      </Section>

      <Section title="八、政策变更与联系方式">
        <p>本政策如有变更，我们会在本页面更新并标注日期。继续使用本服务即视为接受更新后的政策。</p>
        <p>
          联系方式：
          <a href="mailto:privacy@kada.click" className="text-brand-ink hover:text-brand">
            privacy@kada.click
          </a>
        </p>
      </Section>
    </>
  );
}

function EnglishPolicy() {
  return (
    <>
      <Section title="1. What we collect">
        <ul className="space-y-2">
          <Row label="Account">Your phone number. It is your sign-in credential and the only identifier we hold.</Row>
          <Row label="Sign-in record">
            The time and source IP of your last sign-in, used for account security and abuse prevention.
          </Row>
          <Row label="SMS codes">
            Only a SHA-256 digest is stored. Codes expire after five minutes, work once, and never appear in
            plaintext in the database or the logs.
          </Row>
          <Row label="Links and clicks">
            The links you create and one record per visit: time, source IP, User-Agent, referer, and the
            platform and region inferred from the User-Agent. This is what the analytics feature is made of.
          </Row>
          <Row label="AI conversations">
            The messages you send to the AI assistant, the replies, and the user id the conversation belongs
            to, so the next visit continues where you left off.
          </Row>
          <Row label="Access logs">
            The server and reverse proxy record the request path, status code and source IP for debugging
            and abuse prevention.
          </Row>
        </ul>
      </Section>

      <Section title="2. How it is used">
        <p>
          Only to run this service: authentication, redirection, click analytics, the AI assistant, and
          keeping the SMS endpoint from being abused.
        </p>
        <p>Your personal information is never sold, rented, or used for advertising outside this service.</p>
      </Section>

      <Section title="3. Who else sees it">
        <ul className="space-y-2">
          <Row label="Aliyun SMS">Your phone number and the code are sent to Aliyun to deliver the message.</Row>
          <Row label="DeepSeek">What you type into the assistant is sent to DeepSeek to produce a reply.</Row>
          <Row label="Our servers">Data lives in this service&apos;s own servers and database, closed to anyone else.</Row>
        </ul>
        <p>We disclose nothing further unless the law requires it.</p>
      </Section>

      <Section title="4. Text messages and rate limits">
        <p>
          Before a code is sent you have to solve a graphical challenge, and three quotas apply: one message
          per phone number every 60 seconds and ten per day; ten per IP address every hour and thirty per
          day. They exist to protect your number from being bombed and to keep the SMS bill from being
          drained by a script.
        </p>
      </Section>

      <Section title="5. How long it is kept">
        <p>
          Account data is kept while the account exists. Verification codes expire after five minutes and are
          purged within the hour. Click records and access logs are kept for 180 days by default. You can
          delete any link you created at any time.
        </p>
      </Section>

      <Section title="6. Your rights">
        <p>
          You can view and change your profile in Settings, and delete your links, folders, tags and API
          tokens. To close your account or export your data, email us and we will respond within 15 working
          days.
        </p>
      </Section>

      <Section title="7. Children">
        <p>This service is not aimed at children under 14. If you are younger, please use it with a guardian.</p>
      </Section>

      <Section title="8. Changes and contact">
        <p>
          If this policy changes, the date above changes with it. Continuing to use the service means
          accepting the updated policy.
        </p>
        <p>
          Contact:{" "}
          <a href="mailto:privacy@kada.click" className="text-brand-ink hover:text-brand">
            privacy@kada.click
          </a>
        </p>
      </Section>
    </>
  );
}
