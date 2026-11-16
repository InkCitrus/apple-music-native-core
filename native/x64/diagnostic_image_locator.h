#pragma once
#include <cstdint>
#include <cstddef>
#include <cstring>
#include <limits>
#include <string>
#include <vector>
struct DiagnosticElfImage {
    std::string name;
    std::vector<unsigned char> header, program_headers;
    uint64_t phoff=0, min_vaddr=0, max_vaddr=0;
};
struct DiagnosticRegion {
    uintptr_t base=0, allocation_base=0;
    size_t size=0;
    bool readable=false;
};
struct DiagnosticLocation {
    uintptr_t base=0;
    size_t size=0;
    std::string name;
    const char* kind="unknown";
    uint64_t allocation_offset=0, alignment_padding=0;
};
// An aligned ELF image can start after an uncommitted prefix in its Windows
// allocation. Walk region metadata, reading only expected executable headers.
// Never treat an allocation-relative address as an ELF virtual address.
// Query returns false for unmapped/unavailable memory; Read is bounded by the
// caller and never logs or saves the bytes it reads.
template<class Query,class Read>
DiagnosticLocation locateDiagnosticElf(uintptr_t pc,const std::vector<DiagnosticElfImage>& candidates,Query query,Read read) {
    DiagnosticRegion owner;
    if(!query(pc,owner)||!owner.allocation_base||pc<owner.allocation_base)return {};
    const uintptr_t allocation=owner.allocation_base;
    uintptr_t cursor=allocation;
    for(unsigned count=0;count<128 && cursor<=pc;++count){
        DiagnosticRegion region;
        if(!query(cursor,region)||region.allocation_base!=allocation||!region.size||region.base>cursor||region.size>std::numeric_limits<uintptr_t>::max()-region.base)break;
        uintptr_t next=region.base+region.size;
        if(next<=cursor)break;
        if(region.readable&&region.size>=64){
            unsigned char header[64]={};
            if(read(region.base,header,sizeof(header))&&!std::memcmp(header,"\x7f" "ELF",4)){
                for(const auto& candidate:candidates){
                    if(candidate.header.size()!=64 || std::memcmp(header,candidate.header.data(),64) || candidate.phoff>1024*1024 || candidate.program_headers.empty() || candidate.program_headers.size()>128*56 || candidate.phoff>std::numeric_limits<uintptr_t>::max()-region.base)continue;
                    std::vector<unsigned char> ph(candidate.program_headers.size());
                    if(!read(region.base+candidate.phoff,ph.data(),ph.size())||ph!=candidate.program_headers||region.base<candidate.min_vaddr)continue;
                    uintptr_t bias=region.base-static_cast<uintptr_t>(candidate.min_vaddr);
                    if(pc<bias||pc-bias<candidate.min_vaddr||pc-bias>=candidate.max_vaddr)continue;
                    return {bias,static_cast<size_t>(candidate.max_vaddr),candidate.name,"elf_image",pc-allocation,region.base-allocation};
                }
            }
        }
        cursor=next;
    }
    return {allocation,0,"unrecognized_mapping","allocation_only",pc-allocation,0};
}
