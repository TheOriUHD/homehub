package dev.homehub.companion.fabric.mixin;

import com.mojang.brigadier.CommandDispatcher;
import dev.homehub.companion.fabric.HomeHubCommand;
import net.minecraft.commands.CommandBuildContext;
import net.minecraft.commands.CommandSourceStack;
import net.minecraft.commands.Commands;
import org.spongepowered.asm.mixin.Final;
import org.spongepowered.asm.mixin.Mixin;
import org.spongepowered.asm.mixin.Shadow;
import org.spongepowered.asm.mixin.injection.At;
import org.spongepowered.asm.mixin.injection.Inject;
import org.spongepowered.asm.mixin.injection.callback.CallbackInfo;

/**
 * Registers /homehub into the vanilla dispatcher as it is built — the
 * dependency-free stand-in for fabric-api's CommandRegistrationCallback.
 */
@Mixin(Commands.class)
public abstract class CommandsMixin {

    @Shadow @Final private CommandDispatcher<CommandSourceStack> dispatcher;

    @Inject(method = "<init>", at = @At("RETURN"))
    private void homehub$register(Commands.CommandSelection selection, CommandBuildContext context, CallbackInfo ci) {
        HomeHubCommand.register(this.dispatcher);
    }
}
