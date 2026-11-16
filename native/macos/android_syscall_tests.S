.text
.p2align 2
.global android_test_gettimeofday
.type android_test_gettimeofday,%function
android_test_gettimeofday:
 mov x1, #0
 mov x8, #169
 svc #0
 ret
.size android_test_gettimeofday, .-android_test_gettimeofday
