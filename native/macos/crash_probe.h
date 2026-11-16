#pragma once
void install_crash_probe(const char* directory,const char* runtime);
void crash_probe_stage(const char* stage);
void register_crash_probe_module(const char* path,void* handle);
