package dev.homehub.companion.fabric.mixin;

import dev.homehub.companion.fabric.HomeHubCompanionFabric;
import net.minecraft.server.MinecraftServer;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;

import java.util.function.BooleanSupplier;

/**
 * Captures the running server instance and per-tick durations for mspt —
 * the dependency-free stand-in for fabric-api's ServerTickEvents.
 */
@Mixin(MinecraftServer.class)
public abstract class MinecraftServerMixin {

    @Inject(method = "tickServer", at = @At("HEAD"))
    private void homehub$tickStart(BooleanSupplier hasTimeLeft, CallbackInfo ci) {
        HomeHubCompanionFabric.onTickStart((MinecraftServer) (Object) this);
    }

    @Inject(method = "tickServer", at = @At("TAIL"))
    private void homehub$tickEnd(BooleanSupplier hasTimeLeft, CallbackInfo ci) {
        HomeHubCompanionFabric.onTickEnd((MinecraftServer) (Object) this);
    }
}
