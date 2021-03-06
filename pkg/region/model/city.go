// Copyright (C) 2018-2020 Hegemonie's AUTHORS
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package region

import (
	"errors"
)

func (s *SetOfCities) Create(id, loc uint64) {
	c := &City{
		Id: id, Cell: loc,
		Units:      make(SetOfUnits, 0),
		Buildings:  make(SetOfBuildings, 0),
		Knowledges: make(SetOfKnowledges, 0),
	}
	s.Add(c)
}

// Return a Unit owned by the current City, given the Unit ID
func (c *City) Unit(id uint64) *Unit {
	return c.Units.Get(id)
}

// Return a Building owned by the current City, given the Building ID
func (c *City) Building(id uint64) *Building {
	return c.Buildings.Get(id)
}

// Return a Knowledge owned by the current City, given the Knowledge ID
func (c *City) Knowledge(id uint64) *Knowledge {
	return c.Knowledges.Get(id)
}

func (c *City) Armies() []*Army {
	return c.armies[:]
}

// Return total Popularity of the current City (permanent + transient)
func (c *City) Popularity(w *World) int64 {
	var pop int64 = c.Pop

	// Add Transient values for Units in the Armies
	for _, a := range c.armies {
		for _, u := range a.Units {
			ut := w.UnitTypeGet(u.Type)
			pop += ut.PopBonus
		}
		pop += w.Definitions.PopBonusArmyAlive
	}

	// Add Transient values for Units in the City
	for _, u := range c.Units {
		ut := w.UnitTypeGet(u.Type)
		pop += ut.PopBonus
	}

	// Add Transient values for Buildings
	for _, b := range c.Buildings {
		bt := w.BuildingTypeGet(b.Type)
		pop += bt.PopBonus
	}

	// Add Transient values for Knowledges
	for _, k := range c.Knowledges {
		kt := w.KnowledgeTypeGet(k.Type)
		pop += kt.PopBonus
	}

	return pop
}

func (c *City) GetProduction(w *World) *CityProduction {
	p := &CityProduction{}
	for i := 0; i < ResourceMax; i++ {
		p.Buildings.Mult[i] = 1.0
		p.Knowledge.Mult[i] = 1.0
		p.Troops.Mult[i] = 1.0
	}
	for _, b := range c.Buildings {
		t := *w.BuildingTypeGet(b.Type)
		for i := 0; i < ResourceMax; i++ {
			p.Buildings.Plus[i] += t.Prod.Plus[i]
			p.Buildings.Mult[i] *= t.Prod.Mult[i]
		}
	}
	for _, u := range c.Units {
		t := *w.UnitTypeGet(u.Type)
		for i := 0; i < ResourceMax; i++ {
			p.Troops.Plus[i] += t.Prod.Plus[i]
			p.Troops.Mult[i] *= t.Prod.Mult[i]
		}
	}

	p.Base = c.Production
	p.Actual = c.Production
	for i := 0; i < ResourceMax; i++ {
		v := float64(p.Base[i])
		v = v * p.Troops.Mult[i]
		v = v * p.Buildings.Mult[i]
		v = v * p.Knowledge.Mult[i]

		vi := int64(v)
		vi = vi + p.Troops.Plus[i]
		vi = vi + p.Buildings.Plus[i]
		vi = vi + p.Knowledge.Plus[i]

		p.Actual[i] = uint64(vi)
	}

	return p
}

func (c *City) GetStock(w *World) *CityStock {
	p := &CityStock{}
	for i := 0; i < ResourceMax; i++ {
		p.Buildings.Mult[i] = 1.0
		p.Knowledge.Mult[i] = 1.0
		p.Troops.Mult[i] = 1.0
	}
	for _, b := range c.Buildings {
		t := *w.BuildingTypeGet(b.Type)
		for i := 0; i < ResourceMax; i++ {
			p.Buildings.Plus[i] += t.Stock.Plus[i]
			p.Buildings.Mult[i] *= t.Stock.Mult[i]
		}
	}

	p.Base = c.StockCapacity
	p.Actual = c.StockCapacity
	p.Usage = c.Stock
	for i := 0; i < ResourceMax; i++ {
		v := float64(p.Base[i])
		v = v * p.Troops.Mult[i]
		v = v * p.Buildings.Mult[i]
		v = v * p.Knowledge.Mult[i]

		vi := int64(v)
		vi = vi + p.Troops.Plus[i]
		vi = vi + p.Buildings.Plus[i]
		vi = vi + p.Knowledge.Plus[i]

		p.Actual[i] = uint64(vi)
	}

	return p
}

// Create an Army made of the Units defending the City
func (c *City) MakeDefence(w *World) *Army {
	a := &Army{
		Id:       w.getNextId(),
		City:     c.Id,
		Cell:     c.Cell,
		Fight:    0,
		Name:     "Wot?",
		Units:    make(SetOfUnits, 0),
		Postures: []int64{int64(c.Id)},
		Targets:  make([]Command, 0),
	}
	w.Live.Armies.Add(a)
	return a
}

// Play one round of local production and return the
func (c *City) ProduceLocally(w *World, p *CityProduction) Resources {
	var prod Resources = p.Actual
	if c.TicksMassacres > 0 {
		mult := MultiplierUniform(w.Definitions.MassacreImpact)
		for i := uint32(0); i < c.TicksMassacres; i++ {
			prod.Multiply(mult)
		}
		c.TicksMassacres--
	}
	return prod
}

func (c *City) Produce(w *World) {
	// Pre-compute the modified values of Stock and Production.
	// We just reuse a functon that already does it (despite it does more)
	prod0 := c.GetProduction(w)
	stock := c.GetStock(w)

	// Make the local City generate resources (and recover the massacres)
	prod := c.ProduceLocally(w, prod0)
	c.Stock.Add(prod)

	if c.Overlord != 0 {
		if c.pOverlord != nil {
			// Compute the expected Tax based on the local production
			var tax Resources = prod
			tax.Multiply(c.TaxRate)
			// Ensure the tax isn't superior to the actual production (to cope with
			// invalid tax rates)
			tax.TrimTo(c.Stock)
			// Then preempt the tax from the stock
			c.Stock.Remove(tax)

			// TODO(jfs): check for potential shortage
			//  shortage := c.Tax.GreaterThan(tax)

			if w.Definitions.InstantTransfers {
				c.pOverlord.Stock.Add(tax)
			} else {
				c.SendResourcesTo(w, c.pOverlord, tax)
			}

			// FIXME(jfs): notify overlord
			// FIXME(jfs): notify c
		}
	}

	// ATM the stock maybe still stores resources. We use them to make the assets evolve.
	// We arbitrarily give the preference to Units, then Buildings and eventually the
	// Knowledge.

	for _, u := range c.Units {
		if u.Ticks > 0 {
			ut := w.UnitTypeGet(u.Type)
			if c.Stock.GreaterOrEqualTo(ut.Cost) {
				c.Stock.Remove(ut.Cost)
				u.Ticks--
				if u.Ticks <= 0 {
					// FIXME(jfs): Notify the City
				}
			}
		}
	}

	for _, b := range c.Buildings {
		if b.Ticks > 0 {
			bt := w.BuildingTypeGet(b.Id)
			if c.Stock.GreaterOrEqualTo(bt.Cost) {
				c.Stock.Remove(bt.Cost)
				b.Ticks--
				if b.Ticks <= 0 {
					// FIXME(jfs): Notify the City
				}
			}
		}
	}

	for _, k := range c.Knowledges {
		if k.Ticks > 0 {
			bt := w.KnowledgeTypeGet(k.Id)
			if c.Stock.GreaterOrEqualTo(bt.Cost) {
				c.Stock.Remove(bt.Cost)
				k.Ticks--
			}
			if k.Ticks <= 0 {
				// FIXME(jfs): Notify the City
			}
		}
	}

	// At the end of the turn, ensure we do not hold more resources than the actual
	// stock capacity (with the effect of all the multipliers)
	c.Stock.TrimTo(stock.Actual)
}

func (c *City) SetUniformTaxRate(nb float64) {
	c.TaxRate = MultiplierUniform(nb)
}

func (c *City) SetTaxRate(m ResourcesMultiplier) {
	c.TaxRate = m
}

func (c *City) LiberateCity(w *World, other *City) {
	pre := other.pOverlord
	if pre == nil {
		return
	}

	other.Overlord = 0
	other.pOverlord = nil

	// FIXME(jfs): Notify 'pre'
	// FIXME(jfs): Notify 'c'
	// FIXME(jfs): Notify 'other'
}

func (c *City) GainFreedom(w *World) {
	pre := c.pOverlord
	if pre == nil {
		return
	}

	c.Overlord = 0
	c.pOverlord = nil

	// FIXME(jfs): Notify 'pre'
	// FIXME(jfs): Notify 'c'
}

func (c *City) ConquerCity(w *World, other *City) {
	if other.pOverlord == c {
		c.pOverlord = nil
		c.Overlord = 0
		c.TaxRate = MultiplierUniform(0)
		return
	}

	//pre := other.pOverlord
	other.pOverlord = c
	other.Overlord = c.Id
	other.TaxRate = MultiplierUniform(w.Definitions.RateOverlord)

	// FIXME(jfs): Notify 'pre'
	// FIXME(jfs): Notify 'c'
	// FIXME(jfs): Notify 'other'
}

func (c *City) SendResourcesTo(w *World, overlord *City, amount Resources) {
	// FIXME(jfs): NYI
}

func (c *City) TransferOwnResources(a *Army, r Resources) error {
	if a.City != c.Id {
		return errors.New("Army not controlled by the City")
	}
	if !c.Stock.GreaterOrEqualTo(r) {
		return errors.New("Insufficient resources")
	}

	c.Stock.Remove(r)
	a.Stock.Add(r)
	return nil
}

func (c *City) TransferOwnUnit(a *Army, units ...uint64) error {
	if len(units) <= 0 || a == nil {
		panic("EINVAL")
	}

	if a.City != c.Id {
		return errors.New("Army not controlled by the City")
	}

	allUnits := make(map[uint64]*Unit)
	for _, uid := range units {
		if _, ok := allUnits[uid]; ok {
			continue
		}
		if u := c.Units.Get(uid); u == nil {
			return errors.New("Unit not found")
		} else {
			allUnits[uid] = u
		}
	}

	for _, u := range allUnits {
		c.Units.Remove(u)
		a.Units.Add(u)
	}
	return nil
}

func (c *City) KnowledgeFrontier(w *World) []*KnowledgeType {
	return w.KnowledgeGetFrontier(c.Knowledges)
}

func (c *City) BuildingFrontier(w *World) []*BuildingType {
	return w.BuildingGetFrontier(c.Popularity(w), c.Buildings, c.Knowledges)
}

// Return a collection of UnitType that may be trained by the current City
// because all the requirements are met.
// Each UnitType 'p' returned validates 'c.UnitAllowed(p)'.
func (c *City) UnitFrontier(w *World) []*UnitType {
	return w.UnitGetFrontier(c.Buildings)
}

// Check the current City has all the requirements to train a Unti of the
// given UnitType.
func (c *City) UnitAllowed(pType *UnitType) bool {
	if pType.RequiredBuilding == 0 {
		return true
	}
	for _, b := range c.Buildings {
		if b.Type == pType.RequiredBuilding {
			return true
		}
	}
	return false
}

// Create a Unit of the given UnitType.
// No check is performed to verify the City has all the requirements.
func (c *City) UnitCreate(w *World, pType *UnitType) uint64 {
	id := w.getNextId()
	u := &Unit{Id: id, Type: pType.Id, Ticks: pType.Ticks, Health: pType.Health}
	c.Units.Add(u)
	return id
}

// Start the training of a Unit of the given UnitType (id).
// The whole chain of requirements will be checked.
func (c *City) Train(w *World, idType uint64) (uint64, error) {
	pType := w.UnitTypeGet(idType)
	if pType == nil {
		return 0, errors.New("Unit Type not found")
	}
	if !c.UnitAllowed(pType) {
		return 0, errors.New("Precondition Failed: no suitable building")
	}

	return c.UnitCreate(w, pType), nil
}

func (c *City) Study(w *World, kId uint64) (uint64, error) {
	pType := w.KnowledgeTypeGet(kId)
	if pType == nil {
		return 0, errors.New("Knowledge Type not found")
	}
	owned := make(map[uint64]bool)
	for _, k := range c.Knowledges {
		if kId == k.Type {
			return 0, errors.New("Already started")
		}
		owned[k.Type] = true
	}
	for _, k := range pType.Conflicts {
		if owned[k] {
			return 0, errors.New("Conflict")
		}
	}
	for _, k := range pType.Requires {
		if !owned[k] {
			return 0, errors.New("Precondition Failed")
		}
	}

	id := w.getNextId()
	c.Knowledges.Add(&Knowledge{Id: id, Type: kId, Ticks: pType.Ticks})
	return id, nil
}

func (c *City) Build(w *World, bId uint64) (uint64, error) {
	pType := w.BuildingTypeGet(bId)
	if pType == nil {
		return 0, errors.New("Building Type not found")
	}
	if pType.Unique {
		for _, b := range c.Buildings {
			if b.Type == bId {
				return 0, errors.New("Building already present")
			}
		}
	}

	// Check the knowledge requirements are met
	owned := make(map[uint64]bool)
	for _, k := range c.Knowledges {
		owned[k.Type] = true
	}
	for _, k := range pType.Conflicts {
		if owned[k] {
			return 0, errors.New("Conflict")
		}
	}
	for _, k := range pType.Requires {
		if !owned[k] {
			return 0, errors.New("Precondition Failed")
		}
	}

	id := w.getNextId()
	c.Buildings.Add(&Building{Id: id, Type: bId, Ticks: pType.Ticks})
	return id, nil
}

func (c *City) Lieges() []*City {
	return c.lieges[:]
}
