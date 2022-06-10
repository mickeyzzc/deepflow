package metadata

import (
	mapset "github.com/deckarep/golang-set"
	"github.com/golang/protobuf/proto"
	"gitlab.yunshan.net/yunshan/metaflow/message/trident"

	models "server/controller/db/mysql"
)

type MacID struct {
	Mac string
	ID  int
}

func newMacID(vif *models.VInterface) *MacID {
	return &MacID{
		Mac: vif.Mac,
		ID:  vif.ID,
	}
}

type NetworkMacs map[int][]*MacID

type IDToNetworkMacs map[int]NetworkMacs

type ServerToNetworkMacs map[string]NetworkMacs

func newNetworkMacs() NetworkMacs {
	return make(NetworkMacs)
}

func (n NetworkMacs) add(data interface{}) {
	vif := data.(*models.VInterface)
	if vif.Mac == "" {
		return
	}
	macID := newMacID(vif)
	id := vif.NetworkID
	if _, ok := n[id]; ok {
		n[id] = append(n[id], macID)
	} else {
		n[id] = []*MacID{macID}
	}
}

func (n NetworkMacs) get(id int) []*MacID {
	return n[id]
}

func newIDToNetworkMacs() IDToNetworkMacs {
	return make(IDToNetworkMacs)
}

func newServerToNetworkMacs() ServerToNetworkMacs {
	return make(ServerToNetworkMacs)
}

func (t IDToNetworkMacs) add(id int, macs NetworkMacs) {
	t[id] = macs
}

func (t IDToNetworkMacs) getSegmentsByID(id int, s *Segment) []*trident.Segment {
	networkMacs, ok := t[id]
	if ok == false {
		return nil
	}
	segments := make([]*trident.Segment, 0, len(networkMacs))
	for networkID, macIDs := range networkMacs {
		macs := make([]string, 0, len(macIDs))
		vifIDs := make([]uint32, 0, len(macIDs))
		for _, macID := range macIDs {
			macs = append(macs, macID.Mac)
			vifIDs = append(vifIDs, uint32(macID.ID))
			s.vtapUsedVInterfaceIDs.Add(macID.ID)
		}
		segment := &trident.Segment{
			Id:          proto.Uint32(uint32(networkID)),
			Mac:         macs,
			InterfaceId: vifIDs,
		}
		segments = append(segments, segment)
	}

	return segments
}

func (t ServerToNetworkMacs) add(server string, macs NetworkMacs) {
	t[server] = macs
}

func (t ServerToNetworkMacs) getSegmentsByServer(server string, s *Segment) []*trident.Segment {
	networkMacs, ok := t[server]
	if ok == false {
		return nil
	}
	segments := make([]*trident.Segment, 0, len(networkMacs))
	for networkID, macIDs := range networkMacs {
		macs := make([]string, 0, len(macIDs))
		vifIDs := make([]uint32, 0, len(macIDs))
		for _, macID := range macIDs {
			macs = append(macs, macID.Mac)
			vifIDs = append(vifIDs, uint32(macID.ID))
			s.vtapUsedVInterfaceIDs.Add(macID.ID)
		}
		segment := &trident.Segment{
			Id:          proto.Uint32(uint32(networkID)),
			Mac:         macs,
			InterfaceId: vifIDs,
		}
		segments = append(segments, segment)
	}

	return segments
}

type IDToVifs map[int]mapset.Set

func newIDToVifs() IDToVifs {
	return make(IDToVifs)
}

func (v IDToVifs) add(id int, vifs mapset.Set) {
	if _, ok := v[id]; ok {
		for vif := range vifs.Iter() {
			v[id].Add(vif)
		}
	} else {
		v[id] = vifs.Clone()
	}
}

type Segment struct {
	launchServerToSegments  ServerToNetworkMacs
	hostIDToSegments        IDToNetworkMacs
	gatewayHostIDToSegments IDToNetworkMacs
	allGatewayHostSegments  []*trident.Segment
	vtapUsedVInterfaceIDs   mapset.Set
	notVtapUsedSegments     []*trident.Segment
	// vm所有vif的segment，包含vm上的pod pod_node
	vmIDToSegments IDToNetworkMacs
	// 专属采集器remote segment
	bmDedicatedRemoteSegments []*trident.Segment
	podNodeIDToSegments       IDToNetworkMacs

	vmIDToPodNodeAllVifs IDToVifs
	podNodeIDToAllVifs   IDToVifs
}

func newSegment() *Segment {
	return &Segment{
		launchServerToSegments:    newServerToNetworkMacs(),
		hostIDToSegments:          newIDToNetworkMacs(),
		gatewayHostIDToSegments:   newIDToNetworkMacs(),
		allGatewayHostSegments:    []*trident.Segment{},
		vtapUsedVInterfaceIDs:     mapset.NewSet(),
		notVtapUsedSegments:       []*trident.Segment{},
		vmIDToSegments:            newIDToNetworkMacs(),
		bmDedicatedRemoteSegments: []*trident.Segment{},
		podNodeIDToSegments:       newIDToNetworkMacs(),
		vmIDToPodNodeAllVifs:      newIDToVifs(),
		podNodeIDToAllVifs:        newIDToVifs(),
	}
}

func (s *Segment) GetAllGatewayHostSegments() []*trident.Segment {
	return s.allGatewayHostSegments
}

func (s *Segment) GetNotVtapUsedSegments() []*trident.Segment {
	return s.notVtapUsedSegments
}

func (s *Segment) ClearVTapUsedVInterfaceIDs() {
	s.vtapUsedVInterfaceIDs = mapset.NewSet()
}

func (s *Segment) convertDBInfo(rawData *PlatformRawData) {
	podNodeIDtoPodIDs := rawData.podNodeIDtoPodIDs
	podIDToVifs := rawData.podIDToVifs
	podNodeIDToVmID := rawData.podNodeIDToVmID
	podNodeIDToVifs := rawData.podNodeIDToVifs
	idToPodNode := rawData.idToPodNode

	vmIDToPodNodeAllVifs := newIDToVifs()
	podNodeIDToAllVifs := newIDToVifs()

	for _, podnode := range idToPodNode {
		podnodeID := podnode.ID
		if vifs, ok := podNodeIDToVifs[podnodeID]; ok {
			podNodeIDToAllVifs.add(podnodeID, vifs)
		}
		if podIDs, ok := podNodeIDtoPodIDs[podnodeID]; ok {
			for podID := range podIDs.Iter() {
				id := podID.(int)
				if vifs, ok := podIDToVifs[id]; ok {
					podNodeIDToAllVifs.add(podnodeID, vifs)
				}
			}
		}
	}
	for podnodeID, vmID := range podNodeIDToVmID {
		if allVifs, ok := podNodeIDToAllVifs[podnodeID]; ok {
			vmIDToPodNodeAllVifs.add(vmID, allVifs)
		}
	}
	s.podNodeIDToAllVifs = podNodeIDToAllVifs
	s.vmIDToPodNodeAllVifs = vmIDToPodNodeAllVifs
}

func (s *Segment) generateBaseSegmentsFromDB(rawData *PlatformRawData) {
	launchServerToSegments := newServerToNetworkMacs()
	hostIDToSegments := newIDToNetworkMacs()
	gatewayHostIDToSegments := newIDToNetworkMacs()
	vmIDToSegments := newIDToNetworkMacs()
	podNodeIDToSegments := newIDToNetworkMacs()

	for server, vmids := range rawData.serverToVmIDs {
		netWorkMacs := newNetworkMacs()
		for vmid := range vmids.Iter() {
			id := vmid.(int)
			if vmVifs, ok := rawData.vmIDToVifs[id]; ok {
				for vmVif := range vmVifs.Iter() {
					netWorkMacs.add(vmVif)
				}
			}

			if allVifs, ok := s.vmIDToPodNodeAllVifs[id]; ok {
				for allVif := range allVifs.Iter() {
					netWorkMacs.add(allVif)
				}
			}
		}
		launchServerToSegments[server] = netWorkMacs
	}

	for hostID, vifs := range rawData.hostIDToVifs {
		netWorkMacs := newNetworkMacs()
		for hVif := range vifs.Iter() {
			netWorkMacs.add(hVif)
		}
		hostIDToSegments[hostID] = netWorkMacs
	}

	for hostID, vifs := range rawData.gatewayHostIDToVifs {
		netWorkMacs := newNetworkMacs()
		for gVif := range vifs.Iter() {
			netWorkMacs.add(gVif)
		}
		gatewayHostIDToSegments[hostID] = netWorkMacs
	}

	for vmID, vifs := range rawData.vmIDToVifs {
		netWorkMacs := newNetworkMacs()
		for vif := range vifs.Iter() {
			netWorkMacs.add(vif)
		}
		if podVifs, ok := s.vmIDToPodNodeAllVifs[vmID]; ok {
			for podVif := range podVifs.Iter() {
				netWorkMacs.add(podVif)
			}
		}
		vmIDToSegments[vmID] = netWorkMacs
	}

	for podNodeID, vifs := range s.podNodeIDToAllVifs {
		netWorkMacs := newNetworkMacs()
		for vif := range vifs.Iter() {
			netWorkMacs.add(vif)
		}
		podNodeIDToSegments[podNodeID] = netWorkMacs
	}

	s.launchServerToSegments = launchServerToSegments
	s.hostIDToSegments = hostIDToSegments
	s.gatewayHostIDToSegments = gatewayHostIDToSegments
	s.vmIDToSegments = vmIDToSegments
	s.podNodeIDToSegments = podNodeIDToSegments
}

func (s *Segment) generateGatewayHostSegments() {
	segments := make([]*trident.Segment, 0, 1)
	for _, hostSegments := range s.gatewayHostIDToSegments {
		for _, macIDs := range hostSegments {
			macs := make([]string, 0, len(macIDs))
			vifIDs := make([]uint32, 0, len(macIDs))
			for _, macID := range macIDs {
				macs = append(macs, macID.Mac)
				vifIDs = append(vifIDs, uint32(macID.ID))
			}
			segment := &trident.Segment{
				Id:          proto.Uint32(uint32(1)),
				Mac:         macs,
				InterfaceId: vifIDs,
			}
			segments = append(segments, segment)
		}
	}
	s.allGatewayHostSegments = segments
}

func (s *Segment) GenerateNoVTapUsedSegments(rawData *PlatformRawData) {
	macs := []string{}
	vifIDs := []uint32{}
	segments := make([]*trident.Segment, 0, 1)
	for _, vif := range rawData.deviceVifs {
		if !s.vtapUsedVInterfaceIDs.Contains(vif.ID) {
			macs = append(macs, vif.Mac)
			vifIDs = append(vifIDs, uint32(vif.ID))
		}
	}

	if len(macs) > 0 {
		segment := &trident.Segment{
			Id:          proto.Uint32(uint32(1)),
			Mac:         macs,
			InterfaceId: vifIDs,
		}
		segments = append(segments, segment)
	}
	log.Infof("vtap about vifs used: %d  not used: %d",
		s.vtapUsedVInterfaceIDs.Cardinality(), len(macs))
	s.notVtapUsedSegments = segments
}

func (s *Segment) GetLaunchServerSegments(launchServer string) []*trident.Segment {
	return s.launchServerToSegments.getSegmentsByServer(launchServer, s)
}

func (s *Segment) GetVMIDSegments(vmID int) []*trident.Segment {
	return s.vmIDToSegments.getSegmentsByID(vmID, s)
}

func (s *Segment) GetHostIDSegments(hostID int) []*trident.Segment {
	return s.hostIDToSegments.getSegmentsByID(hostID, s)
}

func (s *Segment) GetPodNodeSegments(podNodeID int) []*trident.Segment {
	return s.podNodeIDToSegments.getSegmentsByID(podNodeID, s)
}

func (s *Segment) GetTypeVMSegments(launchServer string, hostID int) []*trident.Segment {
	macs := []string{}
	vifIDs := []uint32{}
	if networkMacs, ok := s.launchServerToSegments[launchServer]; ok {
		for _, macIDs := range networkMacs {
			for _, macID := range macIDs {
				macs = append(macs, macID.Mac)
				vifIDs = append(vifIDs, uint32(macID.ID))
				s.vtapUsedVInterfaceIDs.Add(macID.ID)
			}
		}
	}
	if networkMacs, ok := s.hostIDToSegments[hostID]; ok {
		for _, macIDs := range networkMacs {
			for _, macID := range macIDs {
				macs = append(macs, macID.Mac)
				vifIDs = append(vifIDs, uint32(macID.ID))
				s.vtapUsedVInterfaceIDs.Add(macID.ID)
			}
		}
	}

	segment := &trident.Segment{
		Id:          proto.Uint32(uint32(1)),
		Mac:         macs,
		InterfaceId: vifIDs,
	}
	return []*trident.Segment{segment}
}

func (s *Segment) generateBaseSegments(rawData *PlatformRawData) {
	s.convertDBInfo(rawData)
	s.generateBaseSegmentsFromDB(rawData)
	s.generateGatewayHostSegments()
}
