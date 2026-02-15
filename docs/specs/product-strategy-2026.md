# Maybe Don't AI - Product Strategy & Market Position (February 2026)

> **Purpose**: Product vision, competitive analysis, market positioning, and growth strategy to guide website messaging, product direction, and investment conversations.

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Market Landscape](#2-market-landscape)
3. [Competitive Analysis](#3-competitive-analysis)
4. [Our Differentiation](#4-our-differentiation)
5. [Target Markets & Buyer Personas](#5-target-markets--buyer-personas)
6. [Problem Space & Platform Vision](#6-problem-space--platform-vision)
7. [The Identity Gap: Our Biggest Adjacent Opportunity](#7-the-identity-gap-our-biggest-adjacent-opportunity)
8. [Phased Product Plan](#8-phased-product-plan)
9. [Open Source Strategy (COSS)](#9-open-source-strategy-coss)
10. [Go-to-Market Strategy](#10-go-to-market-strategy)
11. [Investor Positioning](#11-investor-positioning)
12. [Messaging Framework](#12-messaging-framework)
13. [Risks & Mitigations](#13-risks--mitigations)
14. [Website Messaging & Content Strategy](#14-website-messaging--content-strategy)
15. [Key Data Points for Sales & Marketing](#15-key-data-points-for-sales--marketing)
16. [Demo Framework: Selling to Security & Compliance Buyers](#16-demo-framework-selling-to-security--compliance-buyers)

---

## 1. Executive Summary

**Maybe Don't AI** is a guardrails platform for agentic AI. We sit at the protocol layer -- between AI agents and the tools they use -- enforcing security policies on every action an agent takes. Our gateway intercepts MCP (Model Context Protocol) tool calls and CLI commands, evaluating them against deterministic CEL policies and AI-powered validation before allowing execution.

### Why This Matters Now

The AI industry is undergoing a fundamental shift from **read-only** (chatbots answering questions) to **read-write** (agents taking autonomous actions). This shift creates an unprecedented security gap:

- **69% of enterprises** are deploying AI agents, but only **41% have runtime guardrails** (Akto, 2025)
- **87% of enterprises** lack comprehensive AI security frameworks (CSA, 2025)
- **68% of companies** are investing in agentic AI, but only **7.9% can govern these systems at scale** (IDC)
- AI agents now represent the **#1 security concern for CISOs in 2026** (CSO Online)

The market has validated this need with over **$2 billion in AI guardrails acquisitions in 18 months** (Cisco, Palo Alto, CrowdStrike, SentinelOne, F5, Check Point, Proofpoint). But most acquired products were designed for LLM chatbot guardrails, not agentic AI security. The agent-tool boundary -- where MCP lives -- is the new frontier.

### Our Position

We are building the **security layer for agentic AI** -- a self-hosted, policy-driven gateway that gives organizations control over what their AI agents can and cannot do. Our core differentiation:

1. **Self-hosted by design** -- data never leaves the customer's network
2. **Protocol-layer enforcement** -- we operate at the MCP/CLI boundary, not the prompt layer
3. **Dual validation** -- deterministic CEL policies (sub-millisecond) + AI-powered contextual analysis
4. **Single binary deployment** -- zero dependencies, works in air-gapped environments

### Strategy at a Glance

**The facet we own**: Runtime security at the agent-tool boundary -- where agents interact with tools and systems. Not model security, not pre-deployment testing, not posture management. The layer where damage actually happens.

**Platform from the chokepoint**: The gateway position lets us expand incrementally into identity-aware authorization, data loss prevention, supply chain verification, human-in-the-loop approval, and compliance evidence -- all as dimensions of the same traffic we already inspect, shipped as features in the same binary.

**The identity opportunity**: When a user gives an agent a GitHub token, the agent inherits full permissions -- and GitHub can't tell the difference. We solve this with two capabilities: **Agent Sponsor Attestation** (trace every action to the human who authorized the agent) and **Agent Permission Boundaries** (scope down what the agent can do, regardless of the user's downstream credentials). Works with any IdP, any downstream tool.

**Open source (COSS)**: The MCP gateway market is converging on open source. COSS companies raise 1.45x higher at seed, graduate to Series A at 91% vs 48%. We plan to go open-core (Apache 2.0 for core, commercial features for enterprise) to maximize adoption and investor optionality.

**Phased plan**: (1) Identity + DLP now, (2) Supply chain + human-in-the-loop next, (3) Enterprise fleet management + compliance + optional cloud plane later. See Section 8 for details.

---

## 2. Market Landscape

### Market Size

| Metric | Value | Source |
|--------|-------|--------|
| AI Guardrails market (2024) | $0.7B | Market.us |
| AI Guardrails market (2034 projected) | $109.9B (65.8% CAGR) | Market.us |
| AI Governance market (2029 projected) | $5.8B | MarketsandMarkets |
| AI in cybersecurity (2024) | $26.55B | Fortune Business Insights |
| AI in cybersecurity (2032 projected) | $234.64B (31.7% CAGR) | Fortune Business Insights |
| Guardian agents TAM by 2030 | $17-26B (10-15% of agentic AI market) | Gartner |
| Global security spending (2026) | $240B | Gartner |

### Market Category

Gartner has defined **AI Security Platforms (AISPs)** as a Top Strategic Technology Trend for 2026, with two pillars:

1. **AI Usage Control (AIUC)**: Governing how employees and systems interact with AI services
2. **AI Application Cybersecurity (AIAC)**: Securing custom-built AI applications and agents

Gartner predicts >50% of enterprises will use AISPs by 2028 (up from <10% today). They also predict **guardian agents** will capture 10-15% of the agentic AI market by 2030.

### Regulatory Tailwinds

| Regulation | Date | Impact |
|-----------|------|--------|
| EU AI Act -- high-risk rules | August 2, 2026 | Mandatory guardrails for AI in healthcare, law enforcement, education, critical infrastructure. Fines up to 35M EUR or 7% of global revenue. |
| Colorado AI Act | June 30, 2026 | First comprehensive US state AI statute -- impact assessments, consumer disclosures for high-risk AI |
| California TFAIA | January 1, 2026 (active) | Transparency requirements for frontier AI |
| New York RAISE Act | December 2025 (signed) | AI safety and transparency requirements |
| SOC 2 AI criteria | Rolling | Adding AI-specific controls for model governance and data provenance |
| ISO 42001 | Published | AI Management Systems certification -- becoming de facto enterprise AI governance standard |

### The Agentic AI Shift

This is the defining trend: AI is moving from conversation to action.

- **40% of enterprise applications** will feature AI agents by 2026, up from <5% in 2025 (Gartner)
- OWASP published **separate Top 10 lists** for Agentic Applications and for MCP in 2026, recognizing these as distinct security domains
- The MCP ecosystem has exploded: **16,000%+ growth** in servers, **97M+ monthly SDK downloads**, backed by the Agentic AI Foundation (Linux Foundation) with OpenAI, Google, Microsoft, AWS, Anthropic
- Real-world breaches via MCP are already documented: tool poisoning, secret exfiltration, command injection through compromised MCP servers

---

## 3. Competitive Analysis

### The Great Consolidation of 2025

Nearly every well-funded standalone AI guardrails startup was acquired by a large cybersecurity platform in 2024-2025:

| Acquirer | Target | Price | What They Got |
|----------|--------|-------|---------------|
| Cisco | Robust Intelligence | ~$400M | AI Firewall, became "Cisco AI Defense" |
| Palo Alto Networks | Protect AI | ~$500-634M | AI-SPM, model scanning, red teaming |
| CrowdStrike | Pangea | $260M | AI Guard, prompt injection detection |
| SentinelOne | Prompt Security | ~$180-250M | Runtime GenAI protection |
| F5 | CalypsoAI | $180M | AI guardrails, red teaming |
| Check Point | Lakera | ~$300M | Prompt injection defense |
| Proofpoint | Acuvity | Undisclosed | MCP Gateway, agent integrity |

**Total: >$2B spent on AI guardrails companies in ~18 months.**

**What this means for us**: The market is validated at scale. But the acquirers are now integrating these technologies into their existing enterprise platforms -- creating bundle-dependent offerings that are heavy, expensive, and cloud-first. There is a clear gap for a focused, lightweight, self-hosted solution.

### Direct MCP Gateway Competitors

This is our most immediate competitive set:

| Competitor | Deployment | Funding | Strengths | Weaknesses vs. Us |
|-----------|-----------|---------|-----------|-------------------|
| **Lasso Security** (MCP Gateway) | Open-source + commercial | $21M total | First open-source MCP gateway (April 2025); pluggable guardrails | JavaScript/Node.js; less mature policy language; commercial features unclear |
| **Cloudflare** (MCP Server Portals) | SaaS (Open Beta) | Public company | Zero Trust integration; massive infrastructure; prompt injection screening planned | SaaS-only -- customer data flows through Cloudflare; vendor lock-in; no on-premise option |
| **Acuvity** (now Proofpoint) | Enterprise SaaS + on-premise | Acquired Feb 2026 | MCP Gateway with TLS/auth; agent integrity framework | Now part of Proofpoint -- will become part of enterprise suite; integration friction |
| **Composio** (MCP Gateway) | SaaS | $29M | 500+ managed integrations; unified auth; SOC2/ISO | SaaS-only; integration hub more than security gateway |
| **Runlayer** | VPC or Cloud | $11M (Khosla, Felicis) | All-in-one platform; strong VC backing | Cloud-first deployment; newer entrant |
| **Portkey** (MCP Gateway) | SaaS + self-hosted | -- | Extension of established AI Gateway; 60+ guardrails; Gartner Cool Vendor | Broader scope (LLM routing gateway); MCP is an add-on, not core |
| **TrueFoundry** (MCP Gateway) | SaaS + on-prem | -- | Low latency (3-4ms); SOC 2/HIPAA/ITAR | Unified LLM + MCP gateway; security is secondary to orchestration |

### Independent AI Security Companies (Not Acquired)

| Company | Focus | Funding | Relevance |
|---------|-------|---------|-----------|
| **Zenity** | Agentic AI governance | $55M+ (Series B) | Purpose-built for agent security; Gartner Cool Vendor; but SaaS platform, not gateway |
| **Promptfoo** | AI red teaming & testing | $23.4M (Series A) | CI/CD for AI safety; 200K+ developers; open-source core; complementary to us |
| **Arthur AI** | Agent discovery & governance | $63M (Series B) | Evals + guardrails + observability; open-source engine; broader platform |
| **Nightfall AI** | AI DLP | ~$65M (Series B) | DLP-first; AI file classification; strong in data leakage prevention |
| **Guardrails AI** | LLM output validation | $7.5M (Seed) | Open-source framework; application-layer; Python SDK |
| **Noma Security** | AI agent security | $132M (Series B) | 1,300% ARR growth; agentic focus; but SaaS platform |

### Cloud Provider Native Tools

| Provider | Product | Deployment | Limitation for Us |
|----------|---------|-----------|-------------------|
| AWS | Bedrock Guardrails | AWS-only | Locked to AWS; no MCP support; no on-premise |
| Azure | AI Content Safety | Azure-only | Locked to Azure; no MCP support |
| Google | Vertex AI Safety | GCP-only | Locked to GCP; no MCP support |

**Key insight**: Cloud provider tools are ecosystem-locked and focused on their own AI services. They don't address the multi-provider, multi-tool reality of agentic AI.

### Open Source Landscape

| Project | Focus | Relevance |
|---------|-------|-----------|
| **NVIDIA NeMo Guardrails** | Colang DSL for conversation guardrails | Conversation-focused, not action-focused; Python library |
| **Meta LlamaFirewall** | Agent security (PromptGuard, CodeShield) | Strong prompt injection detection; Python; not a gateway |
| **Lasso MCP Gateway** | MCP security proxy | Most direct competitor; JavaScript; open-source |
| **Guardrails AI** | Output validation framework | Application-layer; Python; not protocol-layer |

---

## 4. Our Differentiation

### Primary Differentiators

**1. Self-Hosted, Data-Sovereign by Design**

An MCP gateway inspects every piece of data flowing between AI agents and their tools -- including source code, database queries, API credentials, business logic, and customer data. For a product whose purpose is security inspection, sending that traffic through a third-party SaaS endpoint is paradoxical.

Maybe Don't runs entirely within the customer's infrastructure:
- Single binary, zero external dependencies
- Works in air-gapped environments (defense, government, classified networks)
- Compliant with ITAR, FedRAMP, HIPAA data residency requirements
- No cloud account or internet connection required for core functionality

This isn't just a deployment preference -- it's a trust architecture decision. **65% of governments will introduce technological sovereignty requirements by 2028** (Gartner). Private LLM usage is surging in legal and financial services specifically because of data sovereignty concerns.

**2. Protocol-Layer Enforcement (MCP + CLI)**

Most AI guardrails products operate at the prompt/response layer -- inspecting what goes into and comes out of an LLM. We operate at the **action layer** -- inspecting what an AI agent actually does:

- MCP tool calls: What tool is being called? With what parameters? To which downstream server?
- CLI commands: What shell command is being executed? With what arguments?
- Response content: What data is being returned to the agent?

This is the difference between monitoring what someone says and monitoring what they do. OWASP's new Agentic Applications and MCP Top 10 lists validate that the action layer is a distinct and critical security boundary.

**3. Dual Policy Engine (Deterministic + AI)**

Our hybrid approach combines:
- **CEL (Common Expression Language) policies**: Deterministic, sub-millisecond evaluation. Pattern matching on tool names, parameters, client identifiers. Auditable, predictable, zero false positives when correctly written.
- **AI-powered policies**: Contextual analysis for nuanced decisions. Can evaluate intent, detect social engineering, assess risk of novel tool combinations.

This matters because pure-deterministic guardrails miss context, and pure-AI guardrails are unpredictable and slow. The combination provides defense-in-depth.

**4. Developer-Friendly Architecture**

- Single binary (`maybe-dont`) -- download and run
- YAML configuration, not a web console
- CEL policies are version-controllable, code-reviewable, testable
- Works with any MCP client (Claude Desktop, Cursor, VS Code, custom agents)
- CLI proxy works with any command-line tool (gh, aws, kubectl)
- No vendor lock-in to any AI provider, cloud platform, or development environment

### Secondary Differentiators

- **Multi-client gateway**: Proxy multiple downstream MCP servers through a single gateway with unified policy enforcement
- **Blocking budget**: Configurable latency budget ensures validation doesn't make agents unusably slow
- **Audit logging**: Complete audit trail of every tool call, policy evaluation, and decision
- **Pass-through authentication**: Forward credentials to downstream servers without storing them
- **Native introspection tools**: AI agents can query the gateway itself for audit logs, server status, and security reports

---

## 5. Target Markets & Buyer Personas

### The Buyer: Security, IT, and Compliance Leadership

**Developers are not the primary buyer.** Developers are overconfident in their ability to mitigate AI agent risks, and they don't want added friction in their workflows. The people who understand the enterprise risk -- and have the budget and authority to act -- are CISOs, VPs of Engineering, Heads of IT, and compliance officers.

The deployment model reinforces this: Maybe Don't is **infrastructure the organization deploys**, not a tool individual users opt into. Like a firewall -- you don't choose to use the firewall, you connect through it. The security team deploys the gateway, configures IdP integration and policies, and agents route through it. End users authenticate once and may not even know it's there.

### Why This Extends Beyond Engineering

Engineering teams are often the first to adopt AI agents, but agentic AI is rapidly spreading across the entire organization:

- **Marketing**: AI agents managing campaigns, posting to social media, interacting with CRMs
- **Sales**: AI agents updating Salesforce, sending emails, researching prospects
- **Operations**: AI agents for procurement, vendor management, data processing
- **HR**: AI agents for resume screening, onboarding workflows, benefits administration
- **Finance**: AI agents for reporting, reconciliation, expense approvals

Non-technical users are **less likely to evaluate risk** and **more likely to have access to sensitive data** (customer records, financial data, HR files). A marketing person who gives an AI agent CRM and email access doesn't think twice about the agent potentially emailing every customer in the database. This is a higher-risk scenario than an engineer using Claude Code.

This is an advantage for our positioning: the gateway is invisible infrastructure that protects the entire organization, not just the engineering team.

### Tier 1: Security & IT Leaders at AI-Adopting Organizations

**Buyer**: CISO, VP of Engineering, Head of IT, Director of Security

**Profile**: Organizations where AI agents are being adopted across teams -- engineering first, but spreading to other departments. Leadership recognizes the risk but lacks tooling to govern agent behavior.

**Pain point**: "Our teams are deploying AI agents with access to production systems, customer data, and business tools. I have no visibility into what these agents do and no way to enforce policies."

**Entry motion**: Security/IT team evaluates as part of AI governance initiative. Demo to CISO or VPE showing identity-attributed audit logs and policy enforcement. POC deployed on internal infrastructure.

**Key stats**:
- 81% of employees use unapproved AI tools (Shadow AI)
- Only 28% of organizations can trace agent actions back to a human sponsor
- AI agents are the #1 security concern for CISOs in 2026 (CSO Online)
- 58% monitor AI agents but only 37% have containment controls (the governance-containment gap)

### Tier 2: Regulated Industries (Finance, Healthcare, Government)

**Buyer**: Chief Compliance Officer, CISO, VP of Risk

**Profile**: Organizations with strict compliance requirements (SOC 2, HIPAA, PCI, ITAR, FedRAMP) who need to adopt AI agents without violating regulatory mandates.

**Pain point**: "We need to enable AI-assisted operations across the business, but our compliance framework requires audit trails, access controls, and data residency for all tooling -- including AI."

**Entry motion**: Compliance team evaluates as part of EU AI Act / state regulation preparedness. On-premise deployment model is a hard requirement. Identity-attributed audit logs are the key demo.

**Key stats**:
- EU AI Act high-risk rules take effect August 2026 -- fines up to 35M EUR or 7% of global revenue
- AI-associated breaches cost >$650K per breach (IBM)
- Air-gapped environments are a hard requirement for defense, intelligence, and critical infrastructure
- SOC 2 adding AI-specific criteria; ISO 42001 becoming de facto AI governance standard

### Tier 3: Platform Teams Building AI Products

**Buyer**: VP of Engineering, CTO, Head of Product Security

**Profile**: Companies building AI-powered products that need guardrails on their own agents' behavior -- customer-facing AI systems where a security incident means customer data exposure.

**Pain point**: "We're building AI features into our product and need to ensure our agents can't take unauthorized actions, leak customer data, or be manipulated via tool poisoning."

**Entry motion**: Technical evaluation during AI product architecture phase. Gateway integrated into production infrastructure.

---

## 6. Problem Space & Platform Vision

### The Full Problem Space

Securing agentic AI is a multi-layer problem. No single company will solve all of it. The market is organizing into distinct layers, each with its own leaders:

| Layer | What It Solves | Current Leaders | Our Play |
|-------|---------------|-----------------|----------|
| **AI Model Security** | Is the model safe? Scanning, adversarial robustness | HiddenLayer, Protect AI (Palo Alto) | Not our focus |
| **Pre-deployment Testing** | Red teaming, eval, adversarial testing before production | Promptfoo, Virtue AI | Complementary -- partner |
| **AI Posture Management** | Discovery: what AI exists in my org? | Wiz AI-SPM, Noma Security | Not our focus |
| **AI Governance / Policy** | Organizational policy, risk frameworks, compliance paperwork | Credo AI, Securiti (Veeam) | We generate evidence for their frameworks |
| **Runtime Enforcement** | What can agents do? Stop bad actions in real-time | **This is our facet** | Core product |
| **Observability** | What are agents doing? Analytics, anomaly detection | Datadog, Arize | Natural extension of our audit data |

### Our Facet: Runtime Security at the Agent-Tool Boundary

We own the layer where agents interact with the real world -- calling tools, executing commands, reading and writing data. This is where damage actually happens, and it is where a gateway architecture is the natural enforcement point.

We don't secure the model. We don't test it before deployment. We don't discover shadow AI across the org. **We secure what agents do when they're running -- every tool call, every CLI command, every data flow. In real time, on your infrastructure.**

This is a coherent, defensible facet with a clear value proposition: testing finds problems before deployment, discovery finds problems across the org, but **runtime enforcement is the only layer that prevents an agent from deleting your production database right now**.

### Vision Statement

**Maybe Don't AI is the security perimeter for agentic AI** -- providing organizations with deterministic, auditable control over every action their AI agents take, deployed entirely within their own infrastructure.

### Platform from the Chokepoint

The gateway position is powerful because if you're the proxy through which all agent-tool traffic flows, you can incrementally expand into adjacent capabilities **without changing the architecture**. Each new capability is another dimension of the same traffic you're already inspecting:

```
                    ┌─────────────────────────────────┐
                    │        WHAT WE HAVE TODAY        │
                    │                                  │
                    │   CEL Policy Engine (deny/allow)  │
                    │   AI Policy Engine (contextual)   │
                    │   Audit Logging                   │
                    │   Multi-client MCP Proxy          │
                    │   CLI Proxy                       │
                    └──────────────┬────────────────────┘
                                   │
              Every expansion below uses the same traffic
                                   │
        ┌──────────┬───────────┬───┴────┬──────────┬──────────┐
        ▼          ▼           ▼        ▼          ▼          ▼
   ┌─────────┐┌────────┐┌─────────┐┌────────┐┌────────┐┌──────────┐
   │Identity ││  DLP   ││ Supply  ││ Human  ││Observe ││Compliance│
   │& Access ││        ││ Chain   ││in Loop ││        ││Evidence  │
   │         ││        ││         ││        ││        ││          │
   │IdP token││Scan    ││Tool pin ││Pause & ││Metrics ││Auto-gen  │
   │validate ││params +││Hash     ││approve ││Export  ││audit     │
   │Tool RBAC││response││Integrity││via     ││SIEM    ││reports   │
   │Cred     ││for PII/││alert on ││webhook ││feed    ││for EU AI │
   │vaulting ││secrets ││change   ││        ││        ││Act, SOC2 │
   └─────────┘└────────┘└─────────┘└────────┘└────────┘└──────────┘
```

Every one of these is incremental work on the same binary. We are not building six products -- we are adding six dimensions to the same traffic inspection. And every one of them benefits from on-premise deployment.

### Why a Small Team Can Credibly Deliver This

1. **The gateway IS the platform.** Each capability is a new validation handler in the same request/response chain. DLP is a CEL rule that matches patterns + an AI rule that detects context. Supply chain is a hash check at initialization. Identity is token validation middleware. These are features, not products.

2. **CEL does the heavy lifting locally.** The deterministic engine handles 80% of cases with zero external dependencies and sub-millisecond latency. The AI engine handles the remaining 20% of nuanced cases. No massive ML infrastructure needed.

3. **We integrate, not build.** We don't build an IdP (we integrate with Okta/Entra/Auth0/FusionAuth). We don't build DLP models (we use CEL patterns and optionally integrate with Presidio). We don't build a SIEM (we export to Splunk/Elastic). The gateway is the **enforcement point**, not the data platform.

4. **Single binary means zero ops burden per feature.** Adding DLP doesn't mean customers deploy a new service. It's a config flag and policy rules. Each new capability ships as a version bump, not a new product.

### The Data Sovereignty Thesis: "Your Data Stays. Our Intelligence Comes to You."

There is one capability where not collecting data creates a limitation: **community threat intelligence**. If MCP server X is compromised, how do we share that knowledge without collecting customer data?

The model: **Publish, don't collect.**

- Maintain a **curated threat feed** of known-bad MCP server hashes, malicious tool descriptions, and compromised packages. Customers pull it like a virus definition file. We never see their data.
- **Optional, anonymous telemetry** (already implemented with opt-in metrics). Extend to include anonymized tool reputation signals -- not what agents did, but population-level signals. "47 gateways flagged this tool description hash as changed this week." No customer data, just aggregate signals.
- This mirrors the CrowdStrike model: the sensor runs locally and protects locally, threat intelligence is centralized, but unlike CrowdStrike, the feed doesn't require customer event data.

This is also a potential revenue lever: free tier gets community intelligence, paid tier gets curated, faster-updating threat feeds with richer context.

---

## 7. The Identity Gap: Our Biggest Adjacent Opportunity

### The Problem

The identity gap in MCP is massive and directly maps to a gateway's architecture:

- **53% of MCP servers** use static secrets; only **8.5% use OAuth** (Astrix, 2025)
- **44% of agents** authenticate with static API keys; **43% with username/password** (Strata, 2025)
- **Only 28%** of organizations can trace agent actions back to a human sponsor
- MCP has **no native tool-level authorization** -- only a draft proposal (SEP-1880)
- **88% of organizations** define "privileged" identities as human only (CyberArk)
- **Only 18%** of security leaders are confident their IAM handles agent identities

### What's Validating This Market

The two largest recent cybersecurity M&A deals are both identity-for-agents plays:

- **CrowdStrike acquired SGNL for $740M** (Jan 2026) -- continuous authorization for agents
- **Palo Alto Networks acquired CyberArk for $25B** (Feb 2026) -- machine identity for AI era
- **Microsoft launched Entra Agent ID** -- first-class agent identities
- Multiple startups raising: Strata Identity, Aembit, Akeyless, Descope -- all building agent identity platforms

### The OAuth Competitive Landscape: What Others Are Building

Several gateways are implementing parts of the MCP auth spec, but nobody combines security enforcement with identity:

| Player | What They Implement | What They Don't |
|--------|-------------------|-----------------|
| **Solo.io (Kgateway)** | MCP auth spec as Resource Server, Protected Resource Metadata (RFC 9728), token validation | No security policy enforcement, no CEL/AI validation, no DLP |
| **Kong** | Gateway-level OAuth/OIDC via plugins, token validation, basic rate limiting | No MCP-specific auth, no tool-level authorization, no audit attribution |
| **AWS AgentCore** | IAM-based authorization for Bedrock agents, managed identity | AWS-locked, no MCP auth spec compliance, no on-premise |
| **Cloudflare** | Zero Trust integration for MCP Server Portals, OAuth relay | SaaS-only, no tool-level RBAC, no policy-as-code |
| **Auth0/WorkOS/Scalekit** | OIDC/OAuth providers adding MCP auth support | IdP only -- no enforcement, no gateway, no audit |

**Critical gap nobody fills**: Cross-App Access (XAA / SEP-990) -- the MCP proposal for agents to present identity tokens when calling tools across application boundaries -- is implemented by **nobody** as of February 2026. Declarative claim-to-tool authorization (mapping OIDC claims to specific tool permissions via policy) is nascent across the entire ecosystem.

**Our differentiator is the combination**: Security policy enforcement (CEL + AI) + MCP auth spec compliance + declarative claim-to-tool authorization + identity-attributed audit trails -- in a single, self-hosted binary. Others do one or two of these. Nobody does all four.

### The Core Problem: Agent Impersonation at Full Privilege

Today, when a user gives an AI agent a GitHub Personal Access Token (PAT), the agent inherits the user's full permissions. GitHub sees the same token whether Jane is calling the API or Jane's AI agent is. There is no mechanism for GitHub -- or any downstream tool provider -- to distinguish between the user and the agent, or to scope down permissions because an agent is making the call.

This is the **agent impersonation problem**: the user has authorized the agent to act on their behalf, but the downstream tool has no concept of "agent-scoped access." The agent gets everything the user has.

**GitHub already enforces what the user can do. Maybe Don't enforces what the agent acting on behalf of the user can do.**

This is the missing layer. And it works with any downstream tool -- GitHub, AWS, Salesforce, Slack -- without any of those tools needing to change.

### Our Position: Two Capabilities the Industry Needs

We frame our identity play as two distinct, buildable capabilities:

#### 1. Agent Sponsor Attestation

**Industry term**: In Identity Governance and Administration (IGA), "access attestation" is the established practice of certifying who has access to what. We extend this to agents: **agent sponsor attestation** certifies which human authorized and is accountable for an agent's actions.

**What it solves**: Only 28% of organizations can trace agent actions to a human sponsor. When an agent deletes a repo or exports customer data, the audit log says "PAT xyz was used" -- not who gave that PAT to an agent or why.

**How it works**: The gateway requires authentication via an OAuth Device Authorization Grant flow before any agent traffic passes through. The user authenticates once via their corporate IdP (Okta, Entra, Auth0, Keycloak, FusionAuth), and every subsequent audit log entry is stamped with their verified identity.

**The workflow**:
1. User starts an MCP client (Claude Desktop, Cursor) pointed at the gateway
2. Gateway detects no valid session, initiates device authorization grant
3. User authenticates via their browser against the corporate IdP
4. Gateway receives and caches the IdP token locally (never leaves the network)
5. Every tool call audit entry now includes: user email, user ID, roles/groups from token claims
6. Token refresh happens transparently; re-auth on expiry

**Why device grant**: It works for CLI/desktop scenarios (no browser redirect needed in the agent itself), it's supported by virtually every IdP, and the token stays local -- no data leaves the customer's infrastructure.

This gives us **named audit trails from day one** -- addressing a concrete compliance gap with a standard protocol flow.

#### 2. Agent Permission Boundaries

**Industry term**: Borrowing from AWS IAM's "Permission Boundaries" -- a ceiling on the maximum permissions an entity can have, regardless of what the underlying policy grants. Google Cloud calls a similar concept "Credential Access Boundaries." We apply the same principle to agents: **Agent Permission Boundaries** define the maximum scope of what an agent can do, regardless of what the user's downstream credentials allow.

**What it solves**: The user's PAT might have `admin` access to GitHub. But should the agent have admin access? Agent Permission Boundaries let the security team define: "Agents acting on behalf of users in the 'engineering' role can use `github__push` and `github__create_issue`, but not `github__delete_repo` or `github__manage_org` -- even though the user's PAT would allow it."

**The dual-token architecture**:

```
┌──────────────┐     ┌───────────────────────────────────────┐     ┌──────────────┐
│  MCP Client  │     │           Maybe Don't Gateway          │     │  Downstream  │
│ (Claude,     │────▶│                                        │────▶│  MCP Server  │
│  Cursor)     │     │  Token 1: IdP Token (OIDC/OAuth)       │     │  (GitHub,    │
│              │     │  ├─ Who is the human sponsor?           │     │   AWS, etc.) │
│              │     │  ├─ What roles/groups do they have?     │     │              │
│              │     │  └─ Used by Maybe Don't for scoping     │     │  Token 2:    │
│              │     │                                        │     │  PAT/API Key │
│              │     │  Token 2: PAT / API Key (pass-through) │     │  (unchanged) │
│              │     │  ├─ User's existing credentials         │     │              │
│              │     │  ├─ Maybe Don't never modifies this     │     │              │
│              │     │  └─ Passed through to downstream tool   │     │              │
│              │     │                                        │     │              │
│              │     │  CEL Policy Evaluation:                 │     │              │
│              │     │  ├─ Read claims from Token 1             │     │              │
│              │     │  ├─ Apply Agent Permission Boundaries    │     │              │
│              │     │  └─ Allow/Deny BEFORE Token 2 is sent   │     │              │
└──────────────┘     └───────────────────────────────────────┘     └──────────────┘
```

**Key architectural points**:
- The MCP client sends **two tokens**: one for Maybe Don't (IdP token with identity and claims), one for the downstream tool (PAT/API key, unchanged)
- Maybe Don't inspects the IdP token to determine what the agent is *allowed* to do
- The PAT determines what the agent *could* do at the downstream tool
- Agent Permission Boundaries are the intersection: what the agent is both allowed to do (by policy) and able to do (by credentials)
- Maybe Don't never modifies the downstream token -- it enforces its own boundary before the call reaches the downstream tool
- This works with **any IdP** and **any downstream tool** without either needing to change

**Example CEL policies for Agent Permission Boundaries**:
```yaml
# Engineering role: can use GitHub read/write, but not admin operations
- name: engineering-agent-boundary
  expression: |
    user.roles.exists(r, r == "engineering") &&
    tool.name.startsWith("github__") &&
    tool.name in ["github__delete_repo", "github__manage_org", "github__transfer_repo"]
  action: deny
  message: "Agent Permission Boundary: engineering agents cannot perform admin GitHub operations"

# Marketing role: can use CRM and email tools, but not engineering tools
- name: marketing-agent-boundary
  expression: |
    user.roles.exists(r, r == "marketing") &&
    (tool.name.startsWith("github__") || tool.name.startsWith("aws__"))
  action: deny
  message: "Agent Permission Boundary: marketing agents cannot access engineering tools"

# All agents: cannot export bulk customer data
- name: no-bulk-data-export
  expression: |
    tool.name.matches(".*export.*") || tool.name.matches(".*bulk.*")
  action: deny
  message: "Agent Permission Boundary: bulk data export requires human execution"
```

**Why this is different from just "RBAC"**: Traditional RBAC controls what a user can access. Agent Permission Boundaries control what an agent can do *with the user's existing access*. The user can still delete the repo themselves -- but the agent can't do it on their behalf. This is a new authorization layer that didn't exist before agents.

### IdP Integration: Pluggable by Design

The Agent Permission Boundaries approach is explicitly IdP-agnostic:

- Claims come from whatever OIDC/OAuth provider the org already uses
- CEL policies reference standard claim fields (`user.roles`, `user.groups`, `user.email`, `user.department`)
- No vendor-specific integration required -- any IdP that issues JWTs with claims works
- FusionAuth Entities, Okta groups, Entra ID roles, Keycloak realm roles -- all map to the same CEL variables

For deeper integration, IdPs like FusionAuth with Entities (client credentials grant) can model agents as entities with explicit permissions. This allows the IdP to manage agent-level permissions that Maybe Don't enforces -- but this is an extension, not a requirement.

### Blog & Content Opportunity

**"Adding Identity to Your MCP Gateway: A Walkthrough with FusionAuth"** -- demonstrates both capabilities end-to-end:

1. Start with anonymous agent traffic (no identity, no boundaries)
2. Add agent sponsor attestation via device grant (now every action has a name on it)
3. Add Agent Permission Boundaries via IdP claims (now agents can only do what their role allows)
4. Show the audit log: who did what, what was blocked, and why

This creates content that ranks for "MCP authentication", "MCP identity", "AI agent identity", "AI agent authorization", and "AI agent permission boundaries."

---

## 8. Phased Product Plan

### Phase 0: What We Have Today

- MCP gateway with CEL + AI policy enforcement (request and response)
- CLI proxy for command validation
- Audit logging with AI-powered analysis and native introspection tools
- Multi-client proxy with pass-through authentication
- Self-hosted single binary, zero dependencies, air-gap capable
- Anonymous opt-in usage metrics

### Phase 1: Identity & Data Protection (Near-Term, Next 2-3 Releases)

**Agent Sponsor Attestation**
- Device Authorization Grant flow for user authentication at the gateway
- Dual-token architecture: IdP token for Maybe Don't (identity + claims), PAT pass-through to downstream (unchanged)
- Local token caching -- identity data never leaves the network
- User identity stamped on every audit log entry (user ID, email, roles)
- Token refresh and session lifecycle management
- Configuration: `authentication.enabled`, `authentication.provider` (OIDC discovery URL)

**Agent Permission Boundaries**
- CEL policy variables populated from OIDC token claims (`user.roles`, `user.groups`, `user.email`, `user.department`)
- Permission boundary policies: scope down what agents can do regardless of downstream PAT privileges
- Example: `user.roles.contains("engineering") && tool.name == "github__delete_repo"` → deny (agent can't delete repos even though the user's PAT allows it)
- IdP-agnostic: works with any OIDC/OAuth provider that issues JWTs with claims
- Client credentials grant support for machine-to-machine (agent entity) authentication
- Integration guide for FusionAuth, Okta, Entra ID

**Data Protection: DLP for MCP Traffic**
- CEL-based pattern detection in tool call parameters and responses (SSN, credit card, API key regex patterns)
- New `redact` policy action -- replace sensitive data with placeholders (`{{EMAIL_1}}`, `{{SSN_1}}`) rather than blocking
- Bi-directional scanning: both request parameters and response content
- AI-powered contextual detection for unstructured PII (optional, uses existing AI engine)
- On-premise advantage: "Your sensitive data never leaves your network -- not even to check if it's sensitive"

### Phase 2: Supply Chain & Oversight (3-6 Months)

**MCP Supply Chain Security**
- Tool pinning: hash tool descriptions and parameter schemas on first connection
- Integrity alerts: detect and flag when tool descriptions change unexpectedly (rug pull detection)
- Tool description scanning for hidden instructions / prompt injection attempts
- Known-bad tool signature feed (downloadable threat intelligence)
- SBOM tracking: record tool capabilities, versions, and dependencies per MCP server

**Human-in-the-Loop Approval**
- New `pending_approval` policy action
- Webhook-based notifications (Slack, Teams, email, generic webhook)
- REST API for approve/deny responses with identity attribution
- Configurable timeout with fail-closed (security-critical) or fail-open (lower risk) behavior
- Risk-tiered: not every action needs approval -- only policy-flagged high-risk operations

**Observability Export**
- OpenTelemetry trace export for agent workflows
- SIEM integration via structured log export (OCSF schema for Splunk/Elastic/Datadog)
- Configurable alerting on anomalous patterns (spike in denials, new tool usage, unusual data volumes)

### Phase 3: Enterprise & Platform (6-12 Months)

**Enterprise Operations**
- Centralized policy management across multiple gateway instances (git-based policy distribution)
- SSO/OIDC for gateway administration (policy authoring RBAC)
- Fleet health monitoring and status aggregation
- Compliance evidence generation: auto-generated reports mapped to EU AI Act, SOC 2, ISO 42001

**Threat Intelligence Feed**
- Curated MCP server reputation data with known-bad signatures
- Anonymous, opt-in community signals for tool behavior patterns
- Regular feed updates as a downloadable artifact (no customer data collection required)
- Free community tier + premium enterprise tier with faster updates and richer context

**Optional Cloud Management Plane**
- Centralized dashboard for managing distributed gateways (policy distribution, fleet health)
- Aggregated, anonymized analytics only -- no customer tool call data ever flows through the cloud
- Following the CrowdStrike/HashiCorp model: local data plane, optional cloud control plane

### Feature Prioritization Rationale

Each Phase 1 feature was chosen for the intersection of **market demand**, **competitive gap**, and **on-premise advantage**:

| Feature | Market Demand | Competitive Gap | On-Prem Advantage | Architecture Fit |
|---------|--------------|-----------------|-------------------|------------------|
| **Agent Sponsor Attestation** | 72% can't trace agent actions to humans | No MCP gateway does this today | Token stored locally, identity never leaves network | Middleware addition to request chain |
| **Agent Permission Boundaries** | $25B CyberArk deal validates identity-for-agents; no tool can distinguish user from agent | Nobody enforces agent-scoped auth at the gateway; MCP has no native tool-level auth | Permissions enforced locally via CEL; dual-token stays on-prem | CEL policy variables from IdP token claims |
| **DLP scanning** | 69% cite AI data leaks as top concern; F5 paid $180M for CalypsoAI | No MCP gateway has deep DLP | Data never sent to cloud for scanning | CEL patterns + AI engine for detection, `redact` action |

---

## 9. Open Source Strategy (COSS)

### The Case for Open Source at Pre-Revenue Seed

The data strongly favors COSS (Commercial Open Source Software) for a pre-revenue AI security company:

**Funding advantages:**
- COSS companies raise **1.45x higher at Seed** vs proprietary peers (Linux Foundation/COSSA/Serena, 2025)
- **91% of COSS startups progress Seed to Series A** (vs 48% for all software -- nearly 2x graduation rate)
- COSS companies are **20% faster at raising Series A** and 34% faster reaching Series B
- **71% of OSS seed companies had zero revenue** at seed -- revenue is explicitly not expected
- Dedicated COSS investors exist: OSS Capital, Open Core Ventures, and dozens more

**Comparable seed rounds:**
| Company | Round | Amount | License | Traction at Seed |
|---------|-------|--------|---------|-----------------|
| Obot AI | Seed | $35M | Open source | MCP gateway, Rancher Labs team |
| Promptfoo | Seed | $5M | MIT (open source) | 25K engineers, a16z led |
| Guardrails AI | Seed | $7.5M | Apache 2.0 | 2,800 GitHub stars, zero revenue |
| Runlayer | Seed | $11M | Closed source | 8 unicorn customers, MCP creator as angel |

**The MCP gateway market is converging on open source:** Docker, Solo.io, Lasso, Obot, Cisco all have OSS MCP gateways. Being closed source is the contrarian position in this market.

### What VCs Look For Pre-Revenue at Seed

| Metric | Median at Seed | Good Target | Notes |
|--------|---------------|-------------|-------|
| GitHub Stars | 28 (median) | 500-1,000+ | Trajectory matters more than absolute count |
| Star Growth (MoM) | ~8% | 10-20% | Best-in-class is 12%+ MoM |
| External Contributors | 25 (median) | 10-30 | Signal of real community |
| Revenue | $0 (71% had none) | $0 is fine | Not expected at seed |
| Production Users | A few | 3-5 companies | Even informal usage counts |
| Community (Slack/Discord) | N/A at seed | 100-300 | Quality over quantity |

Key signals investors DON'T rely on: download counts (inflated by CI/CD), Docker pulls, absolute star count without growth trajectory.

Key signals they DO care about: community engagement quality, production usage signals, inbound interest, market timing.

### Why Open Source Specifically Helps an AI Security Product

1. **Security products demand transparency.** Enterprises evaluating a product that inspects all their AI agent traffic need to verify what it does. Code visibility directly addresses this -- "verifiable trust" instead of "trust us."

2. **Enterprise procurement is easier.** 76% of the average application is already open source. Security teams can evaluate without "request a demo" friction.

3. **Community-contributed policies become a moat.** Like Wazuh's community detection rules or Snyk's vulnerability database, community-sourced CEL policy rules and MCP server integrations create compounding value.

4. **The competitive landscape demands it.** Every other MCP gateway is open source. Being closed source puts adoption at a disadvantage.

### Licensing Recommendation

**Apache 2.0 for the core gateway** with commercial features under proprietary license (classic open-core).

This gives:
- Maximum VC optionality (access to both COSS-specific and generalist VCs)
- Maximum adoption speed and developer trust
- The transparency narrative essential for security products

If competition protection is a concern, **Functional Source License (FSL)** -- created by Sentry, converting to Apache 2.0 after 2 years -- provides transparency + non-compete clause. Gaining traction with Y Combinator startups.

### The Open-Core Boundary

| Open Source (Free) | Commercial (Paid) |
|--------------------|-------------------|
| Core gateway proxy and MCP routing | Advanced AI-powered validation engine |
| CEL policy engine | Curated enterprise policy library |
| Basic audit logging | Advanced analytics, compliance reporting |
| Single-gateway deployment | Multi-gateway fleet management |
| Community policy rules | Premium threat intelligence feed |
| Self-hosted | Optional cloud management plane |
| Basic OIDC authentication | Full IdP integration with entity-based permissions |

At exit, COSS companies average **7x greater valuations at IPO** and **14x at M&A** vs closed-source peers (Linux Foundation/COSSA/Serena, 2025).

---

## 10. Go-to-Market Strategy

### The Playbook: Top-Down Sale, Easy Deployment

Unlike developer tools (Snyk, Docker) that benefit from bottom-up PLG, a security gateway adds friction to the end user's workflow. Developers won't voluntarily route their AI agents through a proxy that might deny tool calls. The buyer is the person responsible for organizational risk -- CISO, VPE, compliance officer -- not the individual developer.

However, unlike traditional enterprise security products, our deployment simplicity (single binary, YAML config) means the **sales cycle can be short** because the POC is trivial: download the binary, point agents at it, see results in minutes.

**Phase 1: Awareness & Authority (Current)**
- Thought leadership content targeting CISOs, security teams, and compliance leaders
- Blog posts on MCP security risks, OWASP MCP Top 10, the identity crisis in agentic AI
- Conference presence at security events (BSides, RSA, Black Hat) not just developer events
- SEO targeting enterprise search terms: "agentic AI security", "AI governance", "AI compliance"
- Open source release to build credibility, community, and trust signal

**Phase 2: Direct Engagement & POC**
- Targeted outreach to security leaders at organizations adopting AI agents
- 30-minute demo framework (see Section 16) showing real policy enforcement and identity attribution
- Free POC: "Deploy on your infrastructure in 15 minutes. See every tool call your agents make."
- Case studies demonstrating policy enforcement across engineering AND non-engineering teams

**Phase 3: Enterprise Expansion**
- Identity integration (OIDC/OAuth) as the upgrade trigger -- "see who is behind every agent action"
- Enterprise features: fleet management, compliance reporting, premium threat feed
- Channel partnerships with IdP vendors (FusionAuth, Okta partners), system integrators
- Compliance certifications (SOC 2, ISO 42001) to remove procurement blockers

**Phase 4: Platform (Optional Cloud)**
- Centralized management plane for multi-gateway fleet management
- Policy distribution and aggregated analytics (no customer data in cloud)
- Premium pricing tier (20-50% above self-hosted)

### Pricing Philosophy

Following the open-core model:

| Tier | Target | Deployment | Price |
|------|--------|-----------|-------|
| **Community** | Organizations evaluating | Self-hosted | Free, open source |
| **Team** | Small/mid-market orgs | Self-hosted | Per-gateway subscription |
| **Enterprise** | Large organizations | Self-hosted + management | Annual contract, custom pricing |
| **Cloud** (future) | Convenience buyers | Vendor-managed | Premium over self-hosted |

Key insight: the free tier is not for individual developers to play with -- it's for **security teams to evaluate and POC**. The upgrade trigger is identity integration and fleet management, not usage limits.

---

## 11. Investor Positioning

### The Opportunity

**Category**: AI Security Platform / Guardian Agent for Agentic AI

**Market timing**: We are at the inflection point where AI moves from conversation to action. The MCP ecosystem has grown 16,000%+ in under 2 years, yet security for this protocol is nearly nonexistent. The window for establishing category leadership is open but narrowing.

**Validation signals**:
- $2B+ in AI guardrails M&A (2024-2025) validates the category at scale
- AI security startup funding tripled from $2.16B (2024) to $6.34B (2025)
- Gartner named AISPs a Top Strategic Technology Trend for 2026
- OWASP published dedicated security standards for both Agentic Applications and MCP
- EU AI Act enforcement (August 2026) creates regulatory urgency

### Revenue Multiples in the Category

- AI acquisitions command average revenue multiples of **24x** (vs 12x for traditional software)
- Cloud security leads with **21.7x average, up to 35.5x in M&A**
- COSS companies average **14x at M&A** vs closed-source peers
- Recent comps: Securiti acquired for $1.72B (~11x revenue), Protect AI for $500-634M (~6x capital invested)

### Why Self-Hosted Is an Advantage (Not a Liability)

The default VC assumption is that SaaS is better. For AI security, this assumption is wrong:

1. **The product inspects ALL agent traffic** -- sending that data through a third-party SaaS contradicts the security value proposition
2. **Regulated industries are the biggest buyers** -- defense, finance, healthcare, government all require or strongly prefer on-premise
3. **Air-gapped environments are a hard requirement** for an addressable market worth billions (government, defense, critical infrastructure)
4. **HashiCorp proved the model** -- primarily self-managed, $294.9M ARR, 50% growth, acquired by IBM for $6.4B
5. **Open-core has structural advantages** -- lower delivery costs, community development contributions, higher trust (customers can inspect security code)

### Likely Acquirers (Based on 2025 Patterns)

The consolidation wave shows who is buying in this space:
- **Palo Alto Networks** -- most aggressive ($500-634M for Protect AI, $25B for CyberArk, $3.35B for Chronosphere in 2025 alone). Building "Prisma AIRS" as AI security platform
- **CrowdStrike** -- acquired Pangea ($260M) and SGNL. Building AI Detection and Response (AIDR)
- **Cisco** -- acquired Robust Intelligence ($400M). "Cisco AI Defense" expanding with agentic guardrails
- **F5 Networks** -- acquired CalypsoAI ($180M). Positioned at application delivery/security intersection
- **Check Point** -- acquired Lakera (~$300M). Building AI Security Center of Excellence
- **Proofpoint** -- acquired Acuvity (Feb 2026). Adding MCP-specific security

### Addressing the "Bigger Plan" Concern

When customers or investors say "the problem is bigger than what you have":

> "You're right. Securing agentic AI is a multi-layer problem. We don't try to do all of it -- we own the runtime enforcement layer, where agents interact with tools and systems. That's where damage actually happens.
>
> From our position as the gateway, we're expanding into data protection, identity-aware authorization, supply chain verification, and compliance evidence -- all as dimensions of the same traffic we already inspect. Each capability ships as features in the same binary, not as new products.
>
> What we explicitly don't do: we don't scan models pre-deployment (that's Promptfoo/Virtue AI's job), we don't discover shadow AI across your org (that's Wiz/Noma's job), and we don't replace your IdP (that's Okta/Entra's job). We integrate with all of them.
>
> Our thesis is that runtime enforcement is the highest-leverage layer because it's the only one that actually stops bad things from happening. Testing finds problems before deployment. Discovery finds problems across the org. But enforcement is the only layer that prevents an agent from deleting your production database right now."

### The Seed Pitch Narrative

> "AI agents are the next application layer, and MCP is their API. But there's a fundamental security gap: when a user gives an agent a GitHub token, the agent inherits the user's full permissions. GitHub can't tell the difference between the user and the agent. Neither can AWS, Salesforce, or any other tool. There is no mechanism to scope down what an agent can do on behalf of a user.
>
> Maybe Don't is the missing layer. We sit between agents and the tools they use, enforcing Agent Permission Boundaries -- the ceiling on what an agent can do, regardless of what the user's credentials allow. GitHub enforces what the user can do. We enforce what the agent can do.
>
> We read identity claims from your existing IdP, enforce permission boundaries via deterministic policies, attribute every agent action to a verified human sponsor, and scan every tool call for sensitive data -- all running as a single binary on your infrastructure. No data ever leaves your network.
>
> This isn't just an engineering problem. AI agents are spreading to marketing, sales, HR, and finance. Non-technical users are higher risk and less likely to evaluate what they're giving agents access to. We protect the entire organization.
>
> We're open source because security tools demand transparency, and the market agrees -- 91% of COSS startups progress from seed to Series A, and every major MCP gateway competitor has gone open source."

### Key Metrics to Track

For fundraising and growth conversations:
- Gateway deployments (total installs)
- Tool calls evaluated per month (volume indicator)
- Policy rules in production across customers
- Developer community size (Discord, GitHub stars, contributors)
- Enterprise pipeline and conversion
- GitHub star growth rate (MoM) -- target 10%+
- Production usage signals (companies running in production)

---

## 12. Messaging Framework

### Positioning Statement

**For CISOs, security leaders, and compliance teams** governing AI agent deployments, Maybe Don't AI is the **self-hosted security gateway** that gives you visibility and policy enforcement over every action AI agents take across your organization. Unlike cloud-only guardrails platforms, Maybe Don't **runs entirely within your infrastructure** so your agent traffic -- including source code, API credentials, customer data, and business logic -- **never leaves your network**.

### Key Messages by Audience

**For CISOs / Heads of Security** (Primary buyer):
> "Your teams are giving AI agents access to production systems, customer data, and business tools. You have no visibility into what those agents do. Maybe Don't changes that -- every tool call logged, every action evaluated against your policies, every agent traced to a verified identity."

> "AI agents are the new shadow IT -- except they don't just access data, they take actions. Maybe Don't is the security perimeter your organization needs before agents spread beyond engineering."

**For VPs of Engineering / CTOs** (Technical champion):
> "AI agents are accelerating your teams, but one bad tool call could delete a repo, expose customer data, or escalate cloud permissions. Maybe Don't enforces your policies at the protocol layer -- deploy in 15 minutes, see results immediately."

> "Deterministic CEL policies for what you know is dangerous. AI-powered analysis for what you haven't thought of yet. All in a single binary, all in version control."

**For Compliance / Risk Officers** (Regulatory champion):
> "EU AI Act enforcement begins August 2026. ISO 42001 is becoming the standard. Maybe Don't provides identity-attributed audit trails, policy-as-code in version control, and on-premise deployment to meet your data residency requirements."

> "Every AI action traced to a person. Every policy decision logged. Every byte on your network. Compliance evidence generated automatically."

**For Board / Executive Audience**:
> "AI agents are spreading across marketing, sales, operations, HR, and finance -- not just engineering. Non-technical users adopting AI agents don't evaluate risk the same way. Maybe Don't is invisible infrastructure that protects the entire organization."

**For Investors**:
> "The AI industry is shifting from chatbots to autonomous agents taking real-world actions. Every organization deploying agents needs a security perimeter. We're building it -- self-hosted, protocol-layer, identity-aware -- and we're the only product that combines security enforcement with MCP auth spec compliance in a single binary."

### Narrative Arc (The CISO Story)

1. **The shift**: AI is moving from conversation to action. It's no longer just engineering -- marketing, sales, operations, and HR are all deploying AI agents that interact with production systems, customer data, and business tools.

2. **The gap**: 69% of enterprises are deploying agents, but only 41% have runtime guardrails. 81% of employees use unapproved AI tools. Non-technical users are less likely to evaluate risk and more likely to have access to sensitive data.

3. **The blind spot**: Most organizations can't answer basic questions: Which agents have access to which systems? Who authorized them? What did they do last Tuesday? Only 28% can trace agent actions back to a human sponsor.

4. **The risk**: OWASP published separate Top 10 lists for Agentic Applications and MCP. Real breaches are documented. The EU AI Act takes effect in 6 months with fines up to 7% of global revenue. The cost of inaction is measurable.

5. **The solution**: Maybe Don't is the security perimeter for agentic AI. We sit at the protocol layer -- between agents and the tools they use -- enforcing your policies, attributing every action to a verified identity, and keeping all data within your infrastructure.

6. **The ask**: Deploy in 15 minutes on your infrastructure. See every tool call your agents make. Start with audit visibility, add policy enforcement, then identity attribution. All in the same binary.

### Brand Alignment: "Maybe Don't" = The Agent Permission Question

The brand name is the value proposition. Every time an AI agent is about to take an action, the question is: **"Maybe don't?"**

The agent CAN delete the repo -- it has the user's credentials. But should it? The agent CAN export the customer list -- the PAT allows it. But should it? The agent CAN send that email with confidential pricing -- nothing stops it. But should it?

**Maybe Don't is the layer that asks and answers this question on every action, for every agent, across the organization.**

This framing should be consistent across all touchpoints:

**Website hero options (brand-aligned)**:
- "Your agents can. But should they?"
- "The agent has the credentials. Maybe Don't has the policies."
- "GitHub enforces what your users can do. We enforce what their agents can do."

**Tagline options**:
- "Agent Permission Boundaries for agentic AI"
- "The security perimeter between agents and the tools they use"
- "Your agents can. We decide if they should."

**Website copy anchors**:
- Feature page header: "Your IdP knows who the agent is. Maybe Don't decides what it's allowed to do."
- Identity page header: "GitHub already enforces what Jane can do. Maybe Don't enforces what Jane's agent can do."
- Data protection header: "The agent can see the data. Maybe Don't decides if it should."
- Compliance header: "Every AI action. Every human sponsor. Every policy decision. On your network."

**Pitch deck title slide options**:
- "Maybe Don't -- Agent Permission Boundaries for Agentic AI"
- "Maybe Don't -- Your agents can. We decide if they should."

### Pitch Deck: Core Concepts to Hit

These are the 7 slides (beyond the standard team/ask/financials) that should anchor the pitch deck. Each one maps to a question investors or buyers will have.

**1. The Agent Impersonation Problem** (The "why now")

> When a user gives an AI agent a GitHub token, the agent inherits the user's full permissions. GitHub can't tell the difference between Jane and Jane's agent. There is no mechanism to scope down what the agent can do. This is true for every tool -- AWS, Salesforce, Slack, your database.

Visual: Show the same token being used by a human (controlled, intentional actions) vs. an agent (autonomous, unpredictable actions). Same credentials, different risk profile.

**2. The Missing Layer** (The market gap)

> IdPs authenticate users. Downstream tools enforce user permissions. But nobody enforces what the AGENT can do with the user's permissions. This is a new authorization layer that didn't exist before agents.

Visual: The three-layer diagram -- IdP (who is this?) → **Maybe Don't (what can the agent do?)** → Downstream tool (what can the user do?). The middle layer is highlighted as the gap.

**3. Agent Permission Boundaries** (The product)

> Maybe Don't sits between agents and tools. It reads identity claims from your IdP and enforces permission boundaries on what agents can do -- independent of what the user's credentials allow. Deterministic CEL policies for known risks. AI-powered analysis for everything else.

Visual: The dual-token architecture diagram. Show Token 1 (IdP → Maybe Don't) and Token 2 (PAT → downstream) as separate flows, with Maybe Don't as the decision point.

**4. Agent Sponsor Attestation** (The compliance story)

> Every AI action is traced to a verified human identity. When the auditor asks "who authorized this agent to access customer data?" you have the answer. When the CISO asks "what did our agents do last Tuesday?" you have the logs.

Visual: Audit log entries showing user email, tool called, policy applied, and decision (allow/deny/redact). Contrast with "before" (anonymous PAT usage, no trail).

**5. It's Not Just Engineering** (The market size expansion)

> AI agents are spreading from engineering to marketing, sales, HR, finance, and operations. Non-technical users are less likely to evaluate risk and more likely to have access to sensitive data. A marketing agent with CRM access is a higher-risk scenario than an engineer with Claude Code.

Visual: Org chart showing agents in every department, each connected to sensitive tools. Highlight that the gateway protects all of them, not just engineering.

**6. Self-Hosted by Design** (The deployment advantage)

> An AI security product that sends all your agent traffic through a third-party cloud contradicts its own purpose. Maybe Don't runs as a single binary on your infrastructure. No cloud account. No internet required. Air-gapped environments supported.

Visual: Architecture comparison -- SaaS competitor (data flows through vendor cloud) vs. Maybe Don't (everything stays on-premise). Highlight the irony.

**7. Platform from the Chokepoint** (The expansion story)

> The gateway sees every tool call. Every new capability -- DLP, supply chain verification, human-in-the-loop, compliance evidence -- is another dimension of the same traffic we already inspect. Each ships as a feature in the same binary. We're not building six products; we're adding six dimensions to one.

Visual: The chokepoint architecture diagram from Section 6, showing current capabilities and planned expansions.

### SEO / Content Themes

High-value search terms to target based on market research:
- "AI guardrails" / "AI agent guardrails"
- "MCP security" / "MCP gateway security"
- "agentic AI security" / "agentic AI governance"
- "AI governance" / "AI compliance"
- "shadow AI" / "shadow AI prevention"
- "AI agent monitoring" / "AI agent observability"
- "on-premise AI security" / "self-hosted AI guardrails"
- "EU AI Act compliance" / "AI compliance framework"
- "AI agent audit trail" / "AI agent identity"
- "AI agent permissions" / "AI agent authorization" / "agent permission boundaries"
- "AI tool use security" / "AI function calling security"
- "prompt injection prevention"
- "CISO AI security" / "AI risk management"

---

## 13. Risks & Mitigations

### Risk 1: MCP Is Superseded by Another Protocol

**Likelihood**: Low in the next 2-3 years. MCP is now under the Linux Foundation (Agentic AI Foundation) with backing from Anthropic, OpenAI, Google, Microsoft, and AWS. 97M+ monthly SDK downloads.

**Mitigation**: Our architecture is protocol-agnostic at the policy layer. CEL policies and AI validation evaluate tool calls regardless of the transport protocol. Adding support for additional protocols (A2A, etc.) is an extension, not a rewrite.

### Risk 2: Large Platform Incumbents Bundle Guardrails for Free

**Likelihood**: Medium. Palo Alto, CrowdStrike, and Cisco are all integrating their acquired guardrails technology. Enterprise customers with existing security platform contracts may get bundled guardrails.

**Mitigation**: Bundled guardrails will be cloud-first and platform-locked. Our self-hosted, protocol-specific approach serves customers who can't or won't use cloud-dependent security. Also, bundled solutions typically lag in feature depth vs. focused products.

### Risk 3: SaaS Competitors Move Faster on Features

**Likelihood**: Medium-High. SaaS products with VC funding can iterate faster on cloud-native features (dashboards, integrations, managed services).

**Mitigation**: Our developer-first approach means we optimize for the features developers need (policies-as-code, CLI-first, git-integrated) rather than enterprise dashboard features. The open-core model allows community contributions. The self-hosted requirement is a feature gate that SaaS competitors cannot easily cross.

### Risk 4: On-Premise Makes Growth Harder to Measure

**Likelihood**: High. Self-hosted products have less visibility into usage than SaaS products.

**Mitigation**: Anonymous, opt-in usage metrics (already implemented). Community engagement metrics. Enterprise contract metrics. The HashiCorp model shows this can work -- they reached $294.9M ARR primarily through self-hosted deployments.

### Risk 5: Open Source Competitors Capture Developer Mindshare

**Likelihood**: Medium. Lasso Security, Docker, Solo.io, and Obot all have open-source MCP gateways.

**Mitigation**: Go open-source ourselves (see Section 9). Compete on developer experience, policy language expressiveness (CEL is more powerful than most alternatives), Go binary simplicity (vs JavaScript/Python competitors), documentation quality, and the data-sovereign positioning. Open-core with Apache 2.0 ensures we're in the conversation, not excluded from it.

### Risk 6: Identity Layer Becomes Commoditized Before We Build It

**Likelihood**: Medium. Auth0, WorkOS, Scalekit are all adding MCP auth support. If token validation becomes trivial, our identity-aware authorization loses differentiation.

**Mitigation**: Token validation is table stakes -- our differentiation is what we do with the identity once validated: tool-level CEL policies, sequence-aware authorization, and identity-attributed audit trails. The enforcement layer is the hard part, not the auth handshake.

---

## 14. Website Messaging & Content Strategy

### Website Structure & Page Messaging

**Homepage**

Hero message (one of):
- "The security perimeter for agentic AI"
- "Know what your AI agents do. Control what they're allowed to."
- "Every AI action audited. Every tool call governed. On your infrastructure."

Sub-hero: "Maybe Don't is a self-hosted security gateway that gives CISOs and security teams visibility and policy enforcement over every action AI agents take across the organization -- engineering, marketing, sales, operations -- so your sensitive data never leaves your network."

Key sections:
1. **The problem** (lead with the CISO's pain): "Your teams are deploying AI agents across the organization. You can't see what they do, can't trace actions to people, and can't enforce policies." Stats: 69% deploying, only 41% with guardrails. 81% using unapproved AI tools. Only 28% can trace agent actions to a human.
2. **The spreading risk**: Visual showing AI agents across departments -- engineering, marketing, sales, HR, finance -- each with access to sensitive systems. "It's not just developers. Agentic AI is spreading to every department, and non-technical users are less likely to evaluate risk."
3. **How it works**: Visual of gateway sitting between agents and tools. CEL + AI dual engine. Identity attribution. "Invisible infrastructure your organization routes through."
4. **Why self-hosted**: Data sovereignty, air-gapped support, compliance. "An AI security gateway that sends your agent traffic through a third-party cloud contradicts its own purpose."
5. **Three capabilities**: Policy enforcement (available), identity attribution (coming soon), data protection (coming soon).
6. **Get started**: "Deploy on your infrastructure in 15 minutes. See every tool call your agents make."

**Product / Features Page**

Organize around the platform capabilities (from Section 6 architecture diagram):

| Capability | Headline | Status |
|-----------|----------|--------|
| Policy Enforcement | "Deterministic rules for what you know. AI for what you don't." | Available |
| Identity & Audit Attribution | "Trace every AI action to a verified identity. Your IdP, our enforcement." | Coming soon |
| Data Protection | "Scan every tool call for sensitive data -- without sending it to the cloud." | Coming soon |
| Supply Chain Security | "Verify the tools your agents use are who they claim to be." | Planned |
| Human-in-the-Loop | "Automated where safe. Human approval where it matters." | Planned |
| Observability | "See everything your agents do. Export to the tools you already use." | Partial (audit logs available) |
| Compliance Evidence | "Auto-generated audit reports for EU AI Act, SOC 2, ISO 42001." | Partial (audit reports available) |

**Use Cases Page**

Four use case stories, reframed around the buyer's organizational concerns:

1. **"Governing AI Agents Across the Organization"** (CISO / Head of IT)
   - Scenario: AI agents are spreading from engineering to marketing, sales, and operations. Each department has agents with access to CRM, email, cloud resources, customer databases.
   - Problem: No visibility, no policy enforcement, no way to trace agent actions to the person who authorized them. 81% of employees use unapproved AI tools.
   - Solution: Gateway deployed as infrastructure -- all agents route through it. Policies enforce what agents can do. Every action logged and attributed to a verified identity.
   - Quote-style: "Our developers adopted AI agents first. Then marketing. Then sales. We needed guardrails before the next department, not after."

2. **"AI Compliance for Regulated Industries"** (Compliance / Risk)
   - Scenario: Financial institution deploying AI agents for operations, customer service, and internal tools. EU AI Act enforcement in August 2026.
   - Problem: Auditors need proof that AI actions are governed, attributed to users, and data-sovereign. Every agent interaction is a potential compliance event.
   - Solution: Identity-attributed audit logs, on-premise deployment, policy-as-code in version control, compliance evidence generation.
   - Quote-style: "Every AI action traced to a person. Every policy in version control. Every byte on our network."

3. **"Securing AI-Powered Development"** (VPE / CTO)
   - Scenario: Engineering teams using Claude Code, Cursor, Copilot with access to GitHub, AWS, databases, and production systems.
   - Problem: One bad tool call could delete a repo, expose credentials, or escalate cloud permissions. Engineers are confident they can manage the risk, but the organization can't afford the tail risk.
   - Solution: Policies block destructive operations, redact secrets from responses, log everything for the security team to audit.
   - Quote-style: "Our engineers move fast with AI. Our guardrails make sure they can't move dangerously."

4. **"Protecting Customer Data in AI Products"** (Product Security)
   - Scenario: SaaS company building AI features that interact with customer data via MCP tools.
   - Problem: AI agent could leak customer PII, execute unauthorized actions, or be manipulated via tool poisoning.
   - Solution: DLP scanning, tool integrity verification, multi-tenant authorization, response validation.
   - Quote-style: "We scan every tool call for customer data before it leaves our network. Our security gateway does it, not a third-party cloud."

**Pricing Page** (when ready)

| Tier | What You Get | Price |
|------|-------------|-------|
| **Community** | Core gateway, CEL policies, audit logging, single-gateway | Free, open source |
| **Team** | AI validation engine, DLP, identity integration, priority support | Per-gateway subscription |
| **Enterprise** | Fleet management, compliance reporting, premium threat feed, SLA | Annual contract |

### SEO Keyword Strategy

**Tier 1: High-intent, high-volume** (target with dedicated pages or pillar content)

| Keyword Cluster | Search Intent | Target Page |
|----------------|---------------|-------------|
| "AI guardrails" / "AI agent guardrails" | Broad category awareness | Homepage, blog pillar |
| "MCP security" / "MCP gateway security" | Protocol-specific security | Dedicated landing page |
| "agentic AI security" | Emerging category term | Blog pillar, features page |
| "AI governance" / "AI compliance" | Enterprise/regulatory | Use cases page |

**Tier 2: Medium-intent, growing** (target with blog posts and guides)

| Keyword Cluster | Search Intent | Content Type |
|----------------|---------------|--------------|
| "shadow AI" / "shadow AI prevention" | Enterprise pain point | Blog post + solution page |
| "AI agent monitoring" / "AI agent observability" | Observability buyers | Blog post |
| "on-premise AI security" / "self-hosted AI guardrails" | Deployment preference | Comparison page |
| "EU AI Act compliance" / "AI compliance framework" | Regulatory urgency | Blog post + guide |
| "AI tool use security" / "AI function calling security" | Technical buyers | Blog post |
| "prompt injection prevention" / "prompt injection MCP" | Security practitioners | Blog post + technical guide |
| "MCP authentication" / "MCP identity" / "MCP OAuth" | Identity-aware buyers | Blog post (FusionAuth walkthrough) |

**Tier 3: Long-tail, high-conversion** (target with technical blog posts)

| Keyword | Content |
|---------|---------|
| "how to secure MCP servers" | Tutorial: deploying Maybe Don't with Claude Desktop |
| "MCP tool poisoning prevention" | Technical deep-dive on supply chain attacks + tool pinning |
| "CEL policy examples AI" | Policy cookbook with real CEL rule examples |
| "OWASP MCP top 10 mitigation" | How Maybe Don't addresses each OWASP MCP risk |
| "AI agent DLP on-premise" | Technical comparison: cloud DLP vs on-premise for agent traffic |
| "AI agent identity FusionAuth" | Blog: Adding Identity to Your MCP Gateway with FusionAuth |
| "AI agent permission boundaries" | Blog: Agent Permission Boundaries -- the missing authorization layer |
| "air-gapped AI security" | Guide: deploying AI agent guardrails in air-gapped environments |

### Blog Content Calendar (First 10 Posts)

Priority-ordered by SEO value and thought leadership impact:

| # | Title | Target Keywords | Goal |
|---|-------|----------------|------|
| 1 | **"The OWASP MCP Top 10: What It Means and How to Mitigate Each Risk"** | OWASP MCP, MCP security | Authority/SEO. Map each OWASP risk to a Maybe Don't capability. |
| 2 | **"Adding Identity to Your MCP Gateway: A Walkthrough with FusionAuth"** | MCP authentication, MCP identity, AI agent identity | Demo audit attribution via device grant, then entity-based tool permissions. Technical walkthrough. |
| 3 | **"Why Your AI Security Gateway Should Run On-Premise"** | on-premise AI security, self-hosted AI guardrails, data sovereignty | Position our deployment model as an advantage, not a limitation. |
| 4 | **"AI Agents Are the New Shadow IT: What Security Teams Need to Know"** | shadow AI, AI agent monitoring | Pain point content targeting CISOs and security teams. |
| 5 | **"Policy-as-Code for AI Agents: CEL Rules in Version Control"** | CEL policy, AI policy-as-code, GitOps AI security | Developer-focused content showing our policy-as-code approach. |
| 6 | **"53% of MCP Servers Use Static Secrets: The Identity Crisis in Agentic AI"** | MCP authentication, AI agent identity crisis | Data-driven thought leadership. Links to our identity roadmap. |
| 7 | **"Securing Claude Code / Cursor / Copilot: A Practical Guide"** | secure Claude Code, secure Cursor AI, AI coding assistant security | High-intent developer traffic. Practical setup guide. |
| 8 | **"DLP for the AI Agent Era: Why Cloud Scanning Is Paradoxical"** | AI DLP, AI data loss prevention, AI data leakage | Position on-premise DLP for agent traffic. Tease upcoming feature. |
| 9 | **"Tool Poisoning in MCP: How It Works and How to Detect It"** | MCP tool poisoning, MCP supply chain attack | Technical deep-dive. Real attack examples. Links to tool pinning feature. |
| 10 | **"EU AI Act Compliance Checklist for AI Agent Deployments"** | EU AI Act compliance, AI compliance checklist | Regulatory urgency content targeting compliance buyers. |

### Website Messaging Principles

1. **Lead with the agent impersonation problem.** The most resonant framing: "Your agent has your credentials. Should it have your permissions?" This is the problem every buyer recognizes immediately.

2. **Use the brand name as the value proposition.** "Maybe Don't" is literally the question the gateway answers on every action. Lean into it: "Your agents can. But should they?" This works on the homepage, in pitch decks, and in every conversation.

3. **Data-driven credibility.** Use specific stats (69%, 53%, $2B+, OWASP) instead of vague claims. Numbers from analyst firms and standards bodies carry more weight than our own assertions.

4. **The buyer is the CISO, not the developer.** Developers are overconfident about risk and don't want friction. Frame everything for the person responsible for organizational risk. Developers are end users, not buyers.

5. **Show, don't just tell, the on-premise advantage.** Don't just say "self-hosted." Explain WHY it matters: "An AI security product that sends all your agent traffic through a third-party cloud contradicts its own purpose."

6. **Acknowledge the larger problem honestly.** Don't pretend we solve everything. "We own runtime enforcement. We integrate with the best tools for testing, discovery, and governance." This builds trust with sophisticated buyers.

7. **Frame identity as the differentiator, not just a feature.** "GitHub enforces what the user can do. Maybe Don't enforces what the agent can do." This one sentence explains the entire product to a non-technical buyer.

8. **It's not just engineering.** Always reference cross-department risk (marketing, sales, HR, finance). Non-technical users with sensitive data are the higher-risk scenario and the bigger market.

9. **Avoid generic AI security language.** Don't say "comprehensive AI security platform." Say "Agent Permission Boundaries for agentic AI." Specificity builds credibility and avoids looking like vaporware.

10. **Open source as trust signal.** Reference code transparency and community contribution as security features, not just business model choices.

---

## 15. Key Data Points for Sales & Marketing

### Use in Pitch Decks, Blog Posts, and Website Copy

**Market validation**:
- $2B+ in AI guardrails M&A in 18 months
- AI security funding tripled from $2.16B to $6.34B (2024-2025)
- Gartner: AISPs are a Top Strategic Technology Trend for 2026
- Gartner: Guardian agents will capture 10-15% of the agentic AI market by 2030 ($17-26B TAM)
- Gartner: >50% of enterprises will use AI Security Platforms by 2028

**The problem**:
- 69% of enterprises deploying agents, only 41% have runtime guardrails
- 87% lack comprehensive AI security frameworks
- 81% of employees use unapproved AI tools (Shadow AI)
- 77% of employees leak data via ChatGPT; GenAI = 32% of all corporate data exfiltration
- AI breaches cost >$650K per incident
- 1,862 MCP servers on public internet without authentication
- 82% of MCP implementations use file system ops prone to path traversal
- OWASP published dedicated Top 10 for both Agentic Applications AND MCP

**Regulatory urgency**:
- EU AI Act high-risk rules: August 2, 2026 -- fines up to 35M EUR or 7% of revenue
- Colorado AI Act: June 30, 2026
- SOC 2 adding AI-specific criteria
- ISO 42001 becoming de facto AI governance standard

**MCP ecosystem growth**:
- 16,000%+ growth in MCP servers in under 2 years
- 97M+ monthly SDK downloads
- 300+ MCP clients
- Backed by Linux Foundation (Agentic AI Foundation)
- OpenAI, Google, Microsoft, AWS all supporting MCP

**The agent impersonation problem**:
- When a user gives an agent a PAT, the agent inherits full user permissions -- no tool can tell the difference
- No mechanism exists for downstream tools (GitHub, AWS, Salesforce) to scope down access because an agent is making the call
- This is the problem Maybe Don't solves: Agent Permission Boundaries -- the ceiling on what agents can do, regardless of user credentials

**Identity gap in MCP**:
- 53% of MCP servers use static secrets; only 8.5% use OAuth (Astrix)
- 44% of agents authenticate with static API keys (Strata)
- Only 28% can trace agent actions to a human sponsor (Agent Sponsor Attestation addresses this)
- 88% of orgs define "privileged" identities as human only (CyberArk)
- Only 18% of security leaders confident IAM handles agent identities
- SGNL acquired by CrowdStrike for $740M (identity for agents)
- CyberArk acquired by Palo Alto for $25B (machine identity for AI era)
- MCP has no native tool-level authorization (SEP-1880 still draft)
- Nobody combines security enforcement + identity + tool-level auth in one gateway (our unique position)

**COSS / open source advantage**:
- 91% of COSS startups progress Seed to Series A (vs 48% overall)
- COSS raises 1.45x higher at Seed vs proprietary
- 7x higher IPO valuations, 14x higher M&A valuations for COSS
- 71% of OSS seed companies had zero revenue at seed
- MCP gateway market converging on open source as default

**On-premise advantage**:
- 65% of governments will introduce technological sovereignty requirements by 2028 (Gartner)
- Air-gapped environments are mandatory for defense, intelligence, healthcare, critical infrastructure
- Private LLM usage surging in legal and financial services due to security concerns
- ITAR, FedRAMP, HIPAA all favor or require on-premise data handling

---

## 16. Demo Framework: Selling to Security & Compliance Buyers

### Demo Philosophy

The demo is not a product walkthrough -- it's a risk scenario that the buyer recognizes, followed by a resolution they can deploy immediately. Every demo should make the buyer think: "This is happening in my organization right now, and I can't see it."

**Target audience**: CISO, VP of Engineering, Head of IT, Compliance Officer
**Demo length**: 30 minutes (15 min scenario + 10 min deep-dive + 5 min next steps)
**Key principle**: Show the problem before the solution. The buyer should feel the pain before you relieve it.

### Demo Structure

#### Act 1: "This Is Happening in Your Organization" (5 minutes)

**Goal**: Establish the risk scenario the buyer already suspects but can't prove.

**Setup**: Show a realistic multi-department AI agent deployment:
- An engineering agent (Claude Code) connected to GitHub, AWS CLI, and a database
- A marketing agent connected to a CRM (HubSpot/Salesforce), email (SendGrid), and social media tools
- A sales agent connected to Salesforce, LinkedIn, and a contract management tool

**The moment**: Execute a series of AI agent actions WITHOUT the gateway:
1. Engineering agent runs `aws iam create-access-key` -- creates persistent credentials
2. Marketing agent calls CRM tool to export the full customer contact list
3. Sales agent sends an email to a prospect with a draft that includes confidential pricing

**Narrate**: "This is what's happening in your organization right now. Your teams gave these agents access to these systems. Nobody reviewed what the agents can do. Nobody logged what they did. Nobody can tell you who authorized it."

**Data point**: "81% of employees use unapproved AI tools. Only 28% of organizations can trace agent actions back to a human. OWASP published a dedicated Top 10 for MCP security this year."

#### Act 2: "Now Let's Add the Gateway" (5 minutes)

**Goal**: Show the gateway as invisible infrastructure -- agents route through it, users don't change their workflow.

**The switch**: Deploy the gateway binary (show the single-binary download, YAML config). Point agents at the gateway. Show that agents work exactly as before -- but now every action is inspected.

**Show the audit log**: Every tool call from Act 1 is now logged with:
- Timestamp, tool name, parameters, downstream server
- Policy evaluation result (allow/deny/redact)
- Latency impact (sub-millisecond for CEL rules)

**Narrate**: "Nothing changed for your users. The agents work the same way. But now you can see every action. This is what your security team has been asking for."

#### Act 3: "Policy Enforcement" (5 minutes)

**Goal**: Show deterministic policy enforcement blocking dangerous operations.

**Demo the same three scenarios from Act 1, now with policies active**:

1. **Engineering**: `aws iam create-access-key` -- **DENIED**. CEL policy: `cli.command == "aws" && cli.arguments.exists(a, a == "create-access-key")`. Show the deny message in the agent's output. Show the audit log entry with the policy name and reason.

2. **Marketing**: Customer list export -- **REDACTED**. Response content scanned by DLP policy. Email addresses replaced with `{{EMAIL_1}}`, `{{EMAIL_2}}`. The agent gets the data structure but not the PII. Show the redacted response.

3. **Sales**: Email with confidential pricing -- **PENDING APPROVAL** (future feature, can describe). High-risk action flagged for human review before execution.

**Narrate**: "Deterministic policies for what you know is dangerous -- sub-millisecond, zero false positives, version-controlled like code. AI-powered analysis for nuanced decisions you haven't written rules for yet."

#### Act 4: "Identity Attribution" (5 minutes)

**Goal**: Show that every action is traced to a verified human identity -- the compliance killer feature.

**Demo the device grant flow**:
1. User opens their MCP client (Claude Desktop / Cursor)
2. Gateway detects no session -- displays device grant URL and code
3. User authenticates via their corporate IdP in the browser (show Okta/Entra/FusionAuth login)
4. Gateway receives the token -- session established
5. Now every audit log entry includes: user email, user ID, roles/groups from token claims

**Show the audit log again**: Same entries from Act 3, but now each one says WHO:
```
{
  "timestamp": "2026-02-14T10:23:45Z",
  "user": "jane.doe@acme.com",
  "roles": ["marketing", "crm-admin"],
  "tool": "hubspot__export_contacts",
  "action": "redact",
  "policy": "dlp-pii-in-responses",
  "message": "PII redacted from response content"
}
```

**Narrate**: "Your existing IdP. Our enforcement. Every AI action in your organization now has a name on it. When the auditor asks 'who authorized this agent to access customer data?' -- you have the answer."

**Show tool-level authorization** (if available): "Jane in marketing can use `hubspot__create_contact` but not `github__delete_repo`. Permissions from her IdP roles, enforced at the gateway."

#### Act 5: "The On-Premise Story" (2 minutes)

**Goal**: Reinforce that everything they just saw runs on THEIR infrastructure.

**Show what doesn't exist**: No cloud dashboard. No SaaS login. No data leaving the network.

**Narrate**: "Everything you just saw -- the policy enforcement, the identity attribution, the audit logs -- runs as a single binary on your infrastructure. No cloud account. No internet connection required. Your agent traffic, your policies, your identity data -- none of it leaves your network. An AI security product that sends all your agent traffic through a third-party cloud contradicts its own purpose."

**For regulated buyers, add**: "This deploys in air-gapped environments. ITAR, FedRAMP, HIPAA data residency -- all satisfied by architecture, not by a compliance checkbox."

#### Close: Next Steps (3 minutes)

**The ask**:
1. "Download the binary. Deploy on your infrastructure in 15 minutes."
2. "Point one team's agents at the gateway. Run in audit-only mode -- no blocking, just visibility."
3. "In a week, review the audit logs together. I'll show you what your agents have been doing."

**Why this works**: The first deployment is zero-risk (audit-only mode means nothing is blocked). The buyer gets immediate value (visibility) without disrupting any team. The follow-up conversation is driven by real data from their own environment.

### Demo Variants by Buyer

| Buyer | Emphasize | De-emphasize |
|-------|-----------|--------------|
| **CISO** | Identity attribution, cross-department risk, audit trail completeness | Technical implementation details |
| **VPE / CTO** | Policy-as-code, CEL expressiveness, latency impact, deployment simplicity | Compliance frameworks |
| **Compliance Officer** | Audit log structure, EU AI Act mapping, data residency, evidence generation | Engineering workflow details |
| **Board / Executive** | Cross-department risk narrative, regulatory timeline, on-premise data story | All technical details |

### Demo Environment Requirements

- **Pre-built policy set**: 5-10 CEL rules covering the demo scenarios (deny destructive ops, redact PII, flag high-risk actions)
- **Mock MCP servers**: Simulated downstream servers that return realistic data (customer records with PII, code repo metadata, cloud resource lists)
- **IdP integration**: FusionAuth (or Keycloak) running locally with 3-4 demo users in different roles
- **Audit log viewer**: Either the native `maybedont__get_audit_log` tool or a simple terminal UI showing log entries in real-time
- **Two MCP clients**: One configured as "Engineering" (Claude Code), one as "Marketing" (generic MCP client) to show cross-department scenario

### Key Objections and Responses

**"Our developers will push back on routing through a proxy."**
> "They don't have a choice -- this is infrastructure, like a firewall. But practically, the latency impact is sub-millisecond for CEL rules and the agent experience is unchanged. They won't notice it's there unless a policy blocks something dangerous."

**"We already have Palo Alto / CrowdStrike / Cisco for AI security."**
> "Those platforms are cloud-first and focused on their own ecosystems. They don't operate at the MCP protocol layer, they don't provide tool-level authorization from your IdP, and they can't run in your air-gapped environments. We complement them -- we're the enforcement point at the agent-tool boundary."

**"Can we start with just visibility, no blocking?"**
> "Absolutely. Deploy in audit-only mode. Every policy logs but doesn't block. Review the data, write policies based on what you see, then flip to enforcement when you're ready. This is the recommended path."

**"How is this different from just reading the agent's logs?"**
> "Agent logs show what the agent decided to do. Our audit logs show what actually happened at the tool boundary -- including what was blocked, what was redacted, and what policy made the decision. We also attribute every action to a human identity, which the agent doesn't do."

---

## Sources

### Market Size & Analyst Reports
- [Market.us - AI Guardrails Market](https://market.us/report/ai-guardrails-market/)
- [MarketsandMarkets - AI Governance](https://www.marketsandmarkets.com/PressReleases/ai-governance.asp)
- [Fortune Business Insights - AI in Cybersecurity](https://www.fortunebusinessinsights.com/artificial-intelligence-in-cybersecurity-market-113125)
- [Gartner - Top Strategic Technology Trends 2026](https://www.gartner.com/en/newsroom/press-releases/2025-10-20-gartner-identifies-the-top-strategic-technology-trends-for-2026)
- [Gartner - Guardian Agents Prediction](https://www.gartner.com/en/newsroom/press-releases/2025-06-11-gartner-predicts-that-guardian-agents-will-capture-10-15-percent-of-the-agentic-ai-market-by-2030)
- [Gartner - Security Spending 2025](https://www.gartner.com/en/newsroom/press-releases/2025-07-29-gartner-forecasts-worldwide-end-user-spending-on-information-security-to-total-213-billion-us-dollars-in-2025)
- [Gartner - Cybersecurity Trends 2026](https://www.gartner.com/en/newsroom/press-releases/2026-02-05-gartner-identifies-the-top-cybersecurity-trends-for-2026)

### Competitive & M&A
- [Latio - Unpacking 2025 AI Security Acquisitions](https://pulse.latio.tech/p/unpacking-the-2025-ai-security-acquisitions)
- [Infosecurity Magazine - Biggest Cybersecurity M&A 2025](https://www.infosecurity-magazine.com/news-features/biggest-cybersecurity-mergers/)
- [Bank Info Security - AI Security M&A](https://www.bankinfosecurity.com/blogs/ai-security-goes-mainstream-as-vendors-spend-heavily-on-ma-p-3953)

### Enterprise Buyer Signals
- [CSO Online - CISO Top 10 Priorities 2026](https://www.csoonline.com/article/4114020/cisos-top-10-cybersecurity-priorities-for-2026.html)
- [CSA - AI Security Governance Report](https://www.helpnetsecurity.com/2025/12/24/csa-ai-security-governance-report/)
- [Akto - State of Agentic AI Security 2025](https://www.akto.io/blog/state-of-agentic-ai-security-2025)
- [UpGuard - State of Shadow AI](https://www.upguard.com/resources/the-state-of-shadow-ai)
- [LayerX - Enterprise AI Data Security Report](https://go.layerxsecurity.com/hubfs/LayerX_Enterprise_AI_and_SaaS_Data_Security_Report.pdf)

### Security Research & Standards
- [OWASP - Top 10 for Agentic Applications 2026](https://genai.owasp.org/resource/owasp-top-10-for-agentic-applications-for-2026/)
- [OWASP - Top 10 for MCP 2026](https://owasp.org/www-project-mcp-top-10/)
- [Martin Fowler - Agentic AI Security](https://martinfowler.com/articles/agentic-ai-security.html)
- [AuthZed - Timeline of MCP Breaches](https://authzed.com/blog/timeline-mcp-breaches)
- [Practical DevSecOps - MCP Security Vulnerabilities](https://www.practical-devsecops.com/mcp-security-vulnerabilities/)

### Regulatory
- [EU AI Act Implementation Timeline](https://artificialintelligenceact.eu/implementation-timeline/)
- [Baker Botts - US AI Law Update 2026](https://www.bakerbotts.com/thought-leadership/publications/2026/january/us-ai-law-update)
- [StateTech - AI Guardrails Mandatory 2026](https://statetechmagazine.com/article/2026/01/ai-guardrails-will-stop-being-optional-2026)

### MCP Ecosystem
- [Anthropic - Agentic AI Foundation](https://www.anthropic.com/news/donating-the-model-context-protocol-and-establishing-of-the-agentic-ai-foundation)
- [The New Stack - Why MCP Won](https://thenewstack.io/why-the-model-context-protocol-won/)
- [Pento - A Year of MCP](https://www.pento.ai/blog/a-year-of-mcp-2025-review)
- [MCP Blog - First Anniversary](http://blog.modelcontextprotocol.io/posts/2025-11-25-first-mcp-anniversary/)
- [PulseMCP - Server Directory](https://www.pulsemcp.com/servers)

### GTM & Business Models
- [Docker PLG Pivot (Sacra)](https://sacra.com/research/docker-plg-pivot/)
- [Snyk to $343M ARR (Reo.dev)](https://www.reo.dev/blog/from-open-source-to-343m-arr-how-snyk-made-developers-its-secret-weapon)
- [HashiCorp IPO S-1 (Meritech Capital)](https://www.meritechcapital.com/blog/hashicorp-ipo-s-1-breakdown)
- [Linux Foundation COSS Report](https://www.prnewswire.com/news-releases/linux-foundation-cossa-and-serena-report-shows-venture-investment-in-open-source-outperforms-proprietary-counterparts-and-benefits-communities-302534109.html)
- [BVP - Developer PLG Strategies](https://www.bvp.com/atlas/how-developer-platforms-scale-with-product-led-growth-strategies)

### Identity & Agent Authentication
- [Astrix - State of MCP Server Security 2025](https://astrix.security/learn/blog/state-of-mcp-server-security-2025/)
- [Strata Identity - The AI Agent Identity Crisis](https://www.strata.io/blog/agentic-identity/the-ai-agent-identity-crisis-new-research-reveals-a-governance-gap/)
- [CyberArk - Machine Identities Outnumber Humans 80:1](https://www.cyberark.com/press/machine-identities-outnumber-humans-by-more-than-80-to-1-new-report-exposes-the-exponential-threats-of-fragmented-identity-security/)
- [CrowdStrike Acquires SGNL ($740M)](https://www.crowdstrike.com/en-us/blog/crowdstrike-to-acquire-sgnl/)
- [Palo Alto + CyberArk ($25B)](https://www.paloaltonetworks.com/company/press/2026/palo-alto-networks-completes-acquisition-of-cyberark-to-secure-the-ai-era)
- [Microsoft Entra Agent ID](https://learn.microsoft.com/en-us/entra/agent-id/identity-professional/microsoft-entra-agent-identities-for-ai-agents)
- [Auth0 - MCP and Auth0: An Agentic Match](https://auth0.com/blog/mcp-and-auth0-an-agentic-match-made-in-heaven/)
- [GitGuardian - OAuth for MCP Enterprise Patterns](https://blog.gitguardian.com/oauth-for-mcp-emerging-enterprise-patterns-for-agent-authorization/)
- [Solo.io - MCP Authorization Is a Non-Starter for Enterprise](https://www.solo.io/blog/mcp-authorization-is-a-non-starter-for-enterprise)
- [Cerbos - MCP Permissions with Policy](https://www.cerbos.dev/blog/mcp-permissions-securing-ai-agent-access-to-tools)
- [Oso - Authorization for AI Agents](https://www.osohq.com/learn/authorization-for-ai-agents-mcp-oauth-21)
- [Pomerium - Secure Access for MCP](https://www.pomerium.com/blog/secure-access-for-mcp)

### COSS / Open Source Strategy
- [Linux Foundation / COSSA / Serena - State of Commercial Open Source 2025](https://www.linuxfoundation.org/press/linux-foundation-cossa-and-serena-report-shows-venture-investment-in-open-source-outperforms-proprietary-counterparts-and-benefits-communities)
- [Redpoint Ventures - How Many Stars Is Enough?](https://www.redpoint.com/content-hub/written/so-how-many-stars-is-enough/)
- [Unusual Ventures - Series A for OSS Companies](https://www.unusual.vc/articles/series-a-fundraising-for-an-open-source-company)
- [Cowboy Ventures - Can My OSS Company Raise Series A?](https://medium.com/cowboy-ventures/can-my-open-source-company-raise-series-a-69a00e56f475)
- [OSS Capital](https://oss.capital/)
- [Open Core Ventures](https://www.opencoreventures.com/)
- [Obot AI $35M Seed (PRNewswire)](https://www.prnewswire.com/news-releases/obot-ai-secures-35m-seed-to-build-enterprise-mcp-gateway-302563687.html)
- [Sentry - Introducing the Functional Source License](https://blog.sentry.io/introducing-the-functional-source-license-freedom-without-free-riding/)

### Feature Expansion Research
- [OWASP - MCP Top 10 2025](https://owasp.org/www-project-mcp-top-10/)
- [Permit.io - Human-in-the-Loop Best Practices](https://www.permit.io/blog/human-in-the-loop-for-ai-agents-best-practices-frameworks-use-cases-and-demo)
- [OpenTelemetry - AI Agent Observability](https://opentelemetry.io/blog/2025/ai-agent-observability/)
- [CISA - 2025 SBOM Minimum Elements](https://www.cisa.gov/resources-tools/resources/2025-minimum-elements-software-bill-materials-sbom)
- [CNCF - Introduction to Policy as Code](https://www.cncf.io/blog/2025/07/29/introduction-to-policy-as-code/)

### Investor Landscape
- [Noma Security $100M (PRNewswire)](https://www.prnewswire.com/news-releases/noma-security-raises-100m-to-drive-adoption-of-ai-agent-security-302518641.html)
- [Straiker $21M Launch (PRNewswire)](https://www.prnewswire.com/news-releases/straiker-launches-with-21-million-to-safeguard-ai-302412224.html)
- [Aventis - AI Valuation Multiples](https://aventis-advisors.com/ai-valuation-multiples/)
- [FinroFCA - Cybersecurity Valuations Mid-2025](https://www.finrofca.com/news/cybersecurity-valuation-mid-2025)
- [Walden Catalyst - Why We Invested in Virtue AI](https://waldencatalyst.com/blog/why-we-invested-in-virtue-ai-building-the-future-of-safe-and-secure-ai)
- [Insight Partners - Promptfoo Scale-Up AI](https://www.insightpartners.com/ideas/promptfoo-scale-up-ai/)
- [Runlayer $11M Seed (TechCrunch)](https://techcrunch.com/2025/11/17/mcp-ai-agent-security-startup-runlayer-launches-with-8-unicorns-11m-from-khoslas-keith-rabois-and-felicis/)


Adding on below a follow up:
# Beyond the MCP gateway: where Maybe Don't AI should place its next bet

**The fastest path to $10K+/month ACV is not building more security features — it's becoming the security control plane for AI agents by bundling permissions, audit trails, and secret management through your existing gateway, then selling to CISOs who already have budget authority and regulatory pressure.** This conclusion emerges from a convergence of signals: AI security tools close enterprise deals in 2–8 weeks (faster than any other AI infrastructure category), CISOs rank "securing AI agents" as their #1 priority (37%), and the insurance industry began excluding AI-related losses effective January 1, 2026 — creating a compliance trigger that shortens sales cycles dramatically. The MCP gateway itself is becoming table stakes; the value is in what you enforce through it.

---

## The market is screaming for agent governance, not just agent security

Enterprise generative AI spending hit **$37 billion in 2025**, a 3.2x year-over-year increase, yet Gartner estimates **40% of agentic AI projects will be cancelled by 2027** due to cost, scaling complexity, or unexpected risks. This gap between ambition and failure is where the real opportunity lives — not in stopping attacks, but in making AI agents governable enough that enterprises actually deploy them at scale.

The agentic AI market sits at roughly **$7.5 billion** today and is projected to reach $93–199 billion by 2032. But here's the critical nuance most market maps miss: **86% of enterprise AI spend still goes to copilots, not autonomous agents**. Agent platforms captured only about $750 million in 2025. The buyer of "agent security" is still emerging. This means Maybe Don't AI is building for a market that's forming right now — which is both the risk and the opportunity.

AI security as a category, however, is already white-hot. Over $8.5 billion flowed into 175 AI security companies in 2024–2025, and a breathtaking wave of M&A validated the space: Palo Alto Networks acquired Protect AI for ~$500M, Cisco bought Robust Intelligence for ~$400M, SentinelOne acquired Prompt Security for ~$250M (a two-year-old company with under $10M in revenue), and Check Point grabbed Lakera. Every major platform security vendor has made at least one AI security acquisition. **The M&A window is open. Small teams with deep MCP expertise are exactly what acquirers want.**

---

## Three features that would sell with minimal friction

After analyzing adjacent opportunities across ten categories — from agent testing to cross-organizational trust federation — three emerge as clear winners based on buyer pain urgency, willingness to pay $10K+/month, competitive density, and fit for a small team with MCP gateway expertise.

**Permission enforcement per agent, per tool, per user** is the single most acute problem. Astrix Research found that **88% of MCP servers require credentials, yet only 8.5% use OAuth** — the rest rely on static API keys and long-lived secrets. In a typical 10,000-person enterprise, roughly 3,056 MCP servers run with zero centralized governance. CyberArk's 2025 survey found 96% of enterprises recognize AI agents as a risk, but fewer than half have governance controls. The buyer is the CISO, the budget line already exists (PAM/IAM, typically $50K–$500K/year), and deployment through a gateway architecture means no agent installs. Runlayer, backed by $11M from Khosla with the MCP protocol creator as advisor, signed dozens of customers in four months doing exactly this. The demand signal is undeniable.

**Immutable audit trails with compliance mapping** is the natural complement. The EU AI Act's first enforcement milestones hit in August 2025, with full high-risk system requirements arriving August 2026. Fines reach €35 million or 7% of global turnover. ISO/Verisk issued standardized insurance exclusions for AI-related losses effective January 2026 — companies that cannot demonstrate AI governance face coverage gaps. ISACA's 2025 report describes auditing agentic AI as a "growing challenge" because agent decision-making lacks traceability. Your gateway already sees every tool call, every prompt, every agent action. Building compliance-ready logging that maps to SOC 2, HIPAA, and EU AI Act frameworks is an incredibly natural extension.

**Credential vaulting and short-lived token issuance** rounds out the bundle. With 53% of MCP servers passing secrets via environment variables, the risk of credential leakage is endemic. Peta ("1Password for AI Agents") and Astrix's open-source MCP Secret Wrapper are early movers, but gateway-level credential injection — where agents never see raw keys — is architecturally superior and uniquely enabled by your existing position in the data path.

The combined positioning: **"The security control plane for AI agents — permissions, audit trails, and secret management for every MCP connection."** This maps to three existing CISO budget lines simultaneously, sells to a single decision-maker, deploys in minutes, and addresses documented, regulation-driven urgency.

---

## What the PLG winners actually did (and what to steal)

The most successful bottom-up security and governance tools share a pattern that Maybe Don't AI can replicate — but with a crucial caveat about patience.

Snyk took **two years and ~50,000 free users** to reach its first $100K ARR. The critical insight was picking one ecosystem (Node.js) and solving one pain point (dependency vulnerabilities) with a tool that **fixed issues rather than just finding them** — auto-generating pull requests with remediation. When Snyk first added a paywall at 5,000 registered developers, self-serve revenue was negligible. The real money came from a "pincer" strategy: bottom-up free usage created Product Qualified Leads, then sales engaged the actual buyer (the CISO) armed with internal usage data showing "your organization already has X developers using Snyk."

Tailscale achieved organic developer love by making security invisible — zero-config VPN where encryption, identity, and access control happen automatically. Their 40,000+ member subreddit (larger than Jira's) and word-of-mouth growth came from individuals using it for personal projects (Pi-hole, Home Assistant, Minecraft servers), then bringing it to work. The lesson: **if engineers use your security tool for personal projects, you've won.**

Datadog proved that even infrastructure monitoring can be PLG if time-to-value is under 15 minutes. They capture 40,000+ trial signups monthly and now have 603 customers paying $1M+/year. Usage-based pricing captures more revenue from power users than seat-based models ever could.

LaunchDarkly offers the most directly transferable playbook: feature flags started as a developer empowerment tool ("deploy with confidence"), then governance (audit logs, mandatory approvals, SOC 2 compliance) was layered on top as the enterprise upsell. The developer hook is speed and confidence; the enterprise check is written for governance. Accounts that started at $500/year expanded to $800–900K over three years.

The exception that proves the rule is Wiz, which hit $100M ARR in 18 months with zero PLG — pure top-down sales to Fortune 500 CISOs. But Wiz had $250M+ in funding, an ex-Microsoft leadership team with deep enterprise credibility, and agentless deployment that showed value in minutes. Without those advantages, PLG is the more replicable path.

The actionable synthesis for Maybe Don't AI: **build an open-source CLI that scans agent configurations and shows their "blast radius" — what each agent can access, what credentials it holds, what tools it can invoke.** Make it `pip install maybe-dont-ai` simple. Free forever for individuals and open-source projects. The scary results ("your agent has read-write access to production databases with a static API key that hasn't rotated in 90 days") motivate action and create urgency. Then sell the control plane that fixes it.

---

## Where the enterprise checks are actually being written

The data on deal velocity is unambiguous. AI security tools close enterprise deals in **2–8 weeks** — faster than any other AI infrastructure category. The reason is structural: CISOs have dedicated budget lines, regulatory pressure creates urgency, and board-level awareness of AI risk is near-universal after high-profile incidents (Replit database deletion, Gemini CLI file deletion, IBM Research documenting agents that "wouldn't hesitate to delete an entire production cluster").

For comparison, AI observability tools take 4–12 weeks for enterprise contracts. AI agent platforms take 1–3 months. AI gateways close quickly but at low ACV ($49/month starting tiers) and face severe commoditization from Kong, Cloudflare, and cloud providers adding native gateway features.

The fastest-growing funded companies in relevant spaces tell the story clearly:

- **Arize AI** raised $131M total ($70M Series C in February 2025), serving the US Navy, Uber, and DoorDash at **$50K–$100K/year ACV** for enterprise observability
- **LangChain** reached $16M ARR (doubling in 16 months) with a $1.25B valuation, but primarily through developer-tier PLG revenue
- **CrewAI** went from zero to $3.2M revenue in ~18 months with 150+ enterprise customers
- **Portkey** achieved $5M revenue on just $3M in seed funding with 13 employees — but gateway economics are brutal

Meanwhile, AI security startups are commanding **$50K–$500K+/year** enterprise contracts and getting acquired at 10–12x their funding. Prompt Security raised $23M, had under $10M revenue, and sold for ~$250M in two years. That's the math that matters for Maybe Don't AI.

Enterprise buyers convert on AI tools at **47%** versus 25% for traditional SaaS, and 76% now prefer buying over building (up from 53% in 2024). The market is ready to write checks — but specifically for tools that address documented, regulated risks with clear single-buyer ownership.

---

## What to avoid and why the window is closing

Several adjacent opportunities sound appealing but would be strategic mistakes. **Standalone AI agent cost management** is a saturated feature, not a product — Portkey, Helicone, TrueFoundry, Datadog, and a dozen others already offer it, often for free. Add cost visibility as a dashboard feature in your gateway, but don't build a product around it.

**AI agent sandbox/testing environments** are infrastructure-heavy, low-ACV, and crowded (E2B, Daytona, Blaxel, plus open-source options). **Cross-organizational agent trust** is theoretically important but 12–18 months from buyer readiness — standards bodies are still writing specs. **AI agent rollback/undo** requires deep integration with databases, file systems, and configurations that play to Rubrik's strengths (they've already shipped Agent Rewind), not a security startup's. **AI agent insurance** is an insurance product, not a software product — but use the insurance exclusion trend as sales ammunition.

**Multi-agent coordination safety** is the most intellectually interesting opportunity — zero commercial tools exist, the Cooperative AI Foundation's 60+ author report documents real risks (miscoordination, collusion, cascading jailbreaks), and first-mover advantage would be massive. But enterprises are still deploying single agents. This is a 2027+ opportunity. Monitor it; don't build for it yet.

The competitive landscape demands urgency. **Runlayer** raised $11M from Khosla Ventures with the MCP protocol creator as advisor and signed dozens of customers in four months. Lasso Security, MintMCP, Portkey, Composio, and TrueFoundry are all building MCP governance features. Cloudflare, Wiz, Docker, and Kong have entered the space. And the $1.3 billion M&A wave means platform vendors are actively shopping for exactly the kind of company Maybe Don't AI is. **The window to establish position is 6–12 months before consolidation narrows the field to 2–3 winners.**

---

## Conclusion: the precise play

The strategic recommendation is specific and time-bound. **Ship permissions + audit trails + secret management as a bundled "AI Agent Security Control Plane" within 3–6 months.** Price it at $10–25K/month for enterprises with 100+ agents/MCP servers, with a compliance reporting add-on for regulated industries at $5–10K/month. Sell exclusively to CISOs, mapping to their existing PAM/IAM and GRC budget lines.

Simultaneously, **launch an open-source agent configuration scanner** that shows every agent's blast radius — permissions, credentials, tool access, and risk score. Make it genuinely free, genuinely useful, and installable in under five minutes. This creates the Snyk-style bottom-up funnel: engineers discover the scary truth about their agent configurations, share results internally, and create the internal champion who pulls Maybe Don't AI into the enterprise procurement process.

The insight that changes everything: the MCP gateway is not the product. It's the delivery mechanism. **What sells is governance** — the ability for a CISO to tell their board, their auditors, and their insurance carrier that every AI agent in the organization operates under least-privilege permissions, generates an immutable audit trail, and never touches a raw credential. That's a $10K+/month product. That's what enterprises are writing checks for today. And maybe that's what Maybe Don't AI should build next.
