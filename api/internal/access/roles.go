// Package access decides who may do what. The five roles and the permission matrix are fixed in code (spec 01);
// the API checks them on every request, so hiding a menu item is never the only protection.
package access

import "slices"

type Role string

const (
	Owner        Role = "owner"
	Manager      Role = "manager"
	FrontDesk    Role = "front_desk"
	Revenue      Role = "revenue"
	Housekeeping Role = "housekeeping"
)

var AllRoles = []Role{Owner, Manager, FrontDesk, Revenue, Housekeeping}

type Permission string

const (
	ReservationsView   Permission = "reservations.view"
	ReservationsEdit   Permission = "reservations.edit"
	CheckIn            Permission = "checkin"
	GuestContactView   Permission = "guests.contact.view"
	PaymentsTake       Permission = "payments.take"
	RefundsRequest     Permission = "refunds.request"
	RefundsApprove     Permission = "refunds.approve"
	RatesEdit          Permission = "rates.edit"
	ChannelsManage     Permission = "channels.manage"
	HousekeepingView   Permission = "housekeeping.view"
	HousekeepingUpdate Permission = "housekeeping.update"
	ExtrasManage       Permission = "extras.manage"
	ReportsView        Permission = "reports.view"
	ReportsRevenueView Permission = "reports.revenue.view"
	UsersManage        Permission = "users.manage"
	OwnersManage       Permission = "owners.manage"
	PaymentsConfigure  Permission = "payments.configure"
	BrandingConfigure  Permission = "branding.configure"
	AuditView          Permission = "audit.view"
)

// matrix lists, for each permission, the roles that have it. It is spec 01's table.
var matrix = map[Permission][]Role{
	ReservationsView:   {Owner, Manager, FrontDesk, Revenue},
	ReservationsEdit:   {Owner, Manager, FrontDesk},
	CheckIn:            {Owner, Manager, FrontDesk},
	GuestContactView:   {Owner, Manager, FrontDesk},
	PaymentsTake:       {Owner, Manager, FrontDesk},
	RefundsRequest:     {Owner, Manager, FrontDesk},
	RefundsApprove:     {Owner, Manager},
	RatesEdit:          {Owner, Manager, Revenue},
	ChannelsManage:     {Owner, Manager, Revenue},
	HousekeepingView:   {Owner, Manager, FrontDesk, Housekeeping},
	HousekeepingUpdate: {Owner, Manager, Housekeeping},
	ExtrasManage:       {Owner, Manager},
	ReportsView:        {Owner, Manager},
	ReportsRevenueView: {Owner, Manager, Revenue},
	UsersManage:        {Owner, Manager},
	OwnersManage:       {Owner},
	PaymentsConfigure:  {Owner},
	BrandingConfigure:  {Owner},
	AuditView:          {Owner, Manager},
}

var AllPermissions = func() []Permission {
	out := make([]Permission, 0, len(matrix))
	for p := range matrix {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}()

// Can reports whether a role has a permission. An unknown role or permission can do nothing.
func Can(r Role, p Permission) bool { return slices.Contains(matrix[p], r) }

func validRole(s string) bool { return slices.Contains(AllRoles, Role(s)) }
