package rbac

// Platform features
const (
	FeatureCRM               Feature = "crm"
	FeatureInvoicing         Feature = "invoicing"
	FeaturePayments          Feature = "payments"
	FeatureSubscriptions     Feature = "subscriptions"
	FeatureHelpdesk          Feature = "helpdesk"
	FeatureKnowledgebase     Feature = "knowledgebase"
	FeatureHR                Feature = "hr"
	FeatureProducts          Feature = "products"
	FeatureInventory         Feature = "inventory"
	FeatureEcommerce         Feature = "ecommerce"
	FeatureMarketing         Feature = "marketing"
	FeatureProposals         Feature = "proposals"
	FeatureProjects          Feature = "projects"
	FeatureLegal             Feature = "legal"
	FeatureAccounting        Feature = "accounting"
	FeatureScheduling        Feature = "scheduling"
	FeatureReporting         Feature = "reporting"
	FeatureCommunications    Feature = "communications"
	FeatureWebsite           Feature = "website"
	FeatureAITools           Feature = "ai_tools"
	FeatureInvestorPortal    Feature = "investor_portal"
	FeatureStrategicPlanning Feature = "strategic_planning"
	FeatureWorkflows         Feature = "workflows"
)

// DefaultPermissionMatrix returns the default feature:action -> minimum role level matrix.
func DefaultPermissionMatrix() PermissionMatrix {
	return PermissionMatrix{
		FeatureCRM: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"delete": RoleLevelManager,
			"export": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureInvoicing: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"send":   RoleLevelMember,
			"void":   RoleLevelManager,
			"delete": RoleLevelAdmin,
			"admin":  RoleLevelAdmin,
		},
		FeaturePayments: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"refund": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureSubscriptions: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"cancel": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureHelpdesk: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelMember,
			"assign":  RoleLevelMember,
			"resolve": RoleLevelMember,
			"delete":  RoleLevelManager,
			"admin":   RoleLevelAdmin,
		},
		FeatureKnowledgebase: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelMember,
			"edit":    RoleLevelMember,
			"publish": RoleLevelManager,
			"delete":  RoleLevelManager,
			"admin":   RoleLevelAdmin,
		},
		FeatureHR: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelManager,
			"edit":    RoleLevelManager,
			"approve": RoleLevelManager,
			"delete":  RoleLevelAdmin,
			"payroll": RoleLevelAdmin,
			"admin":   RoleLevelAdmin,
		},
		FeatureProducts: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"delete": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureInventory: {
			"view":      RoleLevelViewer,
			"adjust":    RoleLevelMember,
			"transfer":  RoleLevelMember,
			"write_off": RoleLevelManager,
			"admin":     RoleLevelAdmin,
		},
		FeatureEcommerce: {
			"view":   RoleLevelViewer,
			"manage": RoleLevelMember,
			"admin":  RoleLevelAdmin,
		},
		FeatureMarketing: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"send":   RoleLevelManager,
			"delete": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureProposals: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelMember,
			"edit":    RoleLevelMember,
			"send":    RoleLevelManager,
			"approve": RoleLevelManager,
			"admin":   RoleLevelAdmin,
		},
		FeatureProjects: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"delete": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureLegal: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelMember,
			"edit":    RoleLevelMember,
			"approve": RoleLevelManager,
			"delete":  RoleLevelAdmin,
			"admin":   RoleLevelAdmin,
		},
		FeatureAccounting: {
			"view":         RoleLevelViewer,
			"create":       RoleLevelMember,
			"edit":         RoleLevelMember,
			"close_period": RoleLevelAdmin,
			"admin":        RoleLevelAdmin,
		},
		FeatureScheduling: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"edit":   RoleLevelMember,
			"delete": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureReporting: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"export": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureCommunications: {
			"view":   RoleLevelViewer,
			"create": RoleLevelMember,
			"send":   RoleLevelMember,
			"delete": RoleLevelManager,
			"admin":  RoleLevelAdmin,
		},
		FeatureWebsite: {
			"view":    RoleLevelViewer,
			"edit":    RoleLevelMember,
			"publish": RoleLevelManager,
			"admin":   RoleLevelAdmin,
		},
		FeatureAITools: {
			"view":  RoleLevelViewer,
			"use":   RoleLevelMember,
			"admin": RoleLevelAdmin,
		},
		FeatureInvestorPortal: {
			"view":       RoleLevelViewer,
			"create":     RoleLevelManager,
			"edit":       RoleLevelManager,
			"manage_cap": RoleLevelAdmin,
			"admin":      RoleLevelOwner,
		},
		FeatureStrategicPlanning: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelManager,
			"edit":    RoleLevelManager,
			"approve": RoleLevelAdmin,
			"admin":   RoleLevelOwner,
		},
		FeatureWorkflows: {
			"view":    RoleLevelViewer,
			"create":  RoleLevelMember,
			"edit":    RoleLevelMember,
			"execute": RoleLevelMember,
			"admin":   RoleLevelAdmin,
		},
	}
}
